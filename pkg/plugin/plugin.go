package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutsPlugin "github.com/argoproj/argo-rollouts/rollout/trafficrouting/plugin/rpc"
	pluginTypes "github.com/argoproj/argo-rollouts/utils/plugin/types"
	consulv1aplha1 "github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/wilkermichael/rollouts-plugin-trafficrouter-consul/pkg/utils"
)

const (
	serviceMetaVersionAnnotation     = "consul.hashicorp.com/service-meta-version"
	filterServiceMetaVersionTemplate = "Service.Meta.version == %s"
)

type ConsulTrafficRouting struct {
	ServiceName      string `json:"serviceName" protobuf:"bytes,1,opt,name=serviceName"`
	CanarySubsetName string `json:"canarySubsetName" protobuf:"bytes,2,opt,name=canarySubsetName"`
	StableSubsetName string `json:"stableSubsetName" protobuf:"bytes,3,opt,name=stableSubsetName"`
}

type RpcPlugin struct {
	K8SClient client.Client
	LogCtx    *logrus.Entry
	IsTest    bool
}

var _ rolloutsPlugin.TrafficRouterPlugin = (*RpcPlugin)(nil)

func (r *RpcPlugin) InitPlugin() pluginTypes.RpcError {
	if r.IsTest {
		return pluginTypes.RpcError{}
	}

	cfg, err := utils.NewKubeConfig()
	if err != nil {
		return pluginTypes.RpcError{ErrorString: err.Error()}
	}
	s := runtime.NewScheme()
	if err := consulv1aplha1.AddToScheme(s); err != nil {
		return pluginTypes.RpcError{ErrorString: err.Error()}
	}
	r.K8SClient, err = client.New(cfg, client.Options{Scheme: s})
	if err != nil {
		return pluginTypes.RpcError{ErrorString: err.Error()}
	}

	r.LogCtx = logrus.NewEntry(logrus.New())

	return pluginTypes.RpcError{}
}

func (r *RpcPlugin) UpdateHash(_ *v1alpha1.Rollout, _, _ string, _ []v1alpha1.WeightDestination) pluginTypes.RpcError {
	return pluginTypes.RpcError{}
}

func (r *RpcPlugin) SetWeight(rollout *v1alpha1.Rollout, desiredWeight int32, _ []v1alpha1.WeightDestination) pluginTypes.RpcError {
	ctx := context.TODO()
	consulConfig, err := getPluginConfig(rollout)
	if err != nil {
		return pluginTypes.RpcError{ErrorString: err.Error()}
	}

	serviceName := consulConfig.ServiceName
	canarySubsetName := consulConfig.CanarySubsetName
	stableSubsetName := consulConfig.StableSubsetName
	serviceMetaVersion := rollout.Spec.Template.GetObjectMeta().GetAnnotations()[serviceMetaVersionAnnotation]

	// This checks that we are performing a canary rollout, it is not
	// an error if this is empty. This will be empty on the initial rollout
	if rollout.Status.Canary == (v1alpha1.CanaryStatus{}) {
		r.LogCtx.Debug("Rollout does not have a CanaryStatus yet", "desiredWeight", desiredWeight)
		return pluginTypes.RpcError{}
	}

	// Get the service resolver
	serviceResolver := &consulv1aplha1.ServiceResolver{}
	if err := r.K8SClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: rollout.GetNamespace()}, serviceResolver, &client.GetOptions{}); err != nil {
		return pluginTypes.RpcError{ErrorString: err.Error()}
	}

	// If the rollout is successful (not aborted) then modify the resolver
	if rolloutAborted(rollout) {
		r.LogCtx.Debug("Updating ServiceResolver for aborted rollout", "canarySubsetName", canarySubsetName, "serviceResolver", serviceResolver)
		serviceResolver, err = r.updateResolverForAbortedRollout(canarySubsetName, *serviceResolver)
		if err != nil {
			return pluginTypes.RpcError{ErrorString: err.Error()}
		}
	} else {
		// Check if the pods have completely rolled over, and we are finished, now set the resolver to the stable version
		if rolloutComplete(rollout) {
			r.LogCtx.Debug("Updating ServiceResolver for completion", "stableSubsetName", stableSubsetName, "canarySubsetName", canarySubsetName, "serviceMetaVersion", serviceMetaVersion, "serviceResolver", serviceResolver)
			serviceResolver, err = r.updateResolverAfterCompletion(stableSubsetName, canarySubsetName, serviceMetaVersion, *serviceResolver)
			if err != nil {
				return pluginTypes.RpcError{ErrorString: err.Error()}
			}
		} else {
			// Update the resolver so that canary subset points to the desired version
			r.LogCtx.Debug("Updating ServiceResolver for rollout", "canarySubsetName", canarySubsetName, "serviceMetaVersion", serviceMetaVersion, "serviceResolver", serviceResolver)
			serviceResolver, err = r.updateResolverForRollouts(canarySubsetName, serviceMetaVersion, *serviceResolver)
			if err != nil {
				return pluginTypes.RpcError{ErrorString: err.Error()}
			}
		}
	}

	serviceSplitter := consulv1aplha1.ServiceSplitter{}
	if err := r.K8SClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: rollout.GetNamespace()}, &serviceSplitter, &client.GetOptions{}); err != nil {
		return pluginTypes.RpcError{ErrorString: err.Error()}
	}

	// Assure tha the split exists
	if len(serviceSplitter.Spec.Splits) == 0 {
		return pluginTypes.RpcError{ErrorString: "spec.splits was not found in consul service splitter"}
	}
	if len(serviceSplitter.Spec.Splits) != 2 {
		return pluginTypes.RpcError{ErrorString: fmt.Sprintf("unexpected number of service splits expected 2 found %d", len(serviceSplitter.Spec.Splits))}
	}

	// We only expect there to be two splits, one for the canary and one for the stable
	for i, split := range serviceSplitter.Spec.Splits {
		switch split.ServiceSubset {
		case canarySubsetName:
			serviceSplitter.Spec.Splits[i].Weight = float32(desiredWeight)
		case stableSubsetName:
			serviceSplitter.Spec.Splits[i].Weight = float32(100 - desiredWeight)
		default:
			return pluginTypes.RpcError{ErrorString: "unexpected service split"}
		}
	}

	// Persist resources at end of function to prevent writing to the cluster if there is an error
	// Persist changes to the ServiceResolver
	if err := r.K8SClient.Update(ctx, serviceResolver, &client.UpdateOptions{}); err != nil {
		return pluginTypes.RpcError{ErrorString: err.Error()}
	}

	// Persist changes to the ServiceSplitter
	r.LogCtx.Debug("Updating ServiceSplitter", "serviceSplitter", serviceSplitter)
	if err := r.K8SClient.Update(ctx, &serviceSplitter, &client.UpdateOptions{}); err != nil {
		return pluginTypes.RpcError{ErrorString: err.Error()}
	}
	return pluginTypes.RpcError{}
}

func (r *RpcPlugin) SetHeaderRoute(_ *v1alpha1.Rollout, _ *v1alpha1.SetHeaderRoute) pluginTypes.RpcError {
	return pluginTypes.RpcError{}
}

func (r *RpcPlugin) VerifyWeight(_ *v1alpha1.Rollout, _ int32, _ []v1alpha1.WeightDestination) (pluginTypes.RpcVerified, pluginTypes.RpcError) {
	return pluginTypes.NotImplemented, pluginTypes.RpcError{}
}

func (r *RpcPlugin) Type() string {
	return Type
}

func (r *RpcPlugin) SetMirrorRoute(_ *v1alpha1.Rollout, _ *v1alpha1.SetMirrorRoute) pluginTypes.RpcError {
	return pluginTypes.RpcError{}
}

func (r *RpcPlugin) RemoveManagedRoutes(ro *v1alpha1.Rollout) pluginTypes.RpcError {
	return pluginTypes.RpcError{}
}

func (r *RpcPlugin) updateResolverAfterCompletion(stableSubsetName, canarySubsetName, serviceMetaVersion string, sr consulv1aplha1.ServiceResolver) (*consulv1aplha1.ServiceResolver, error) {
	var err error
	serviceResolver, err := r.updateCanaryResolverForRollouts(canarySubsetName, fmt.Sprintf(filterServiceMetaVersionTemplate, serviceMetaVersion), sr)
	if err != nil {
		return nil, err
	}

	// Update the resolver so that stable subset points to the former canary version
	if _, ok := serviceResolver.Spec.Subsets[stableSubsetName]; !ok {
		return nil, errors.New(fmt.Sprintf("spec.subsets.%s.filter was not found in consul service resolver: %v", canarySubsetName, sr))
	}
	stableSubset := serviceResolver.Spec.Subsets[stableSubsetName]
	stableSubset.Filter = fmt.Sprintf(filterServiceMetaVersionTemplate, serviceMetaVersion)
	serviceResolver.Spec.Subsets[stableSubsetName] = stableSubset

	return serviceResolver, nil
}

// updateCanaryResolverForRollouts sets the canary filter to the serviceMetaVersion passed in
func (r *RpcPlugin) updateResolverForRollouts(canarySubsetName, serviceMetaVersion string, sr consulv1aplha1.ServiceResolver) (*consulv1aplha1.ServiceResolver, error) {
	return r.updateCanaryResolverForRollouts(canarySubsetName, fmt.Sprintf(filterServiceMetaVersionTemplate, serviceMetaVersion), sr)
}

// updateResolverForAbortedRollout sets the canary filter to empty if we've aborted the rollout
func (r *RpcPlugin) updateResolverForAbortedRollout(canarySubsetName string, sr consulv1aplha1.ServiceResolver) (*consulv1aplha1.ServiceResolver, error) {
	return r.updateCanaryResolverForRollouts(canarySubsetName, "", sr)
}

func (r *RpcPlugin) updateCanaryResolverForRollouts(canarySubsetName, filterValue string, sr consulv1aplha1.ServiceResolver) (*consulv1aplha1.ServiceResolver, error) {
	if _, ok := sr.Spec.Subsets[canarySubsetName]; !ok {
		return nil, errors.New(fmt.Sprintf("spec.subsets.%s.filter was not found in consul service resolver: %v", canarySubsetName, sr))
	}
	canarySubset := sr.Spec.Subsets[canarySubsetName]
	canarySubset.Filter = filterValue
	sr.Spec.Subsets[canarySubsetName] = canarySubset

	return &sr, nil
}

func rolloutComplete(rollout *v1alpha1.Rollout) bool {
	rolloutCondition, err := completeCondition(rollout)
	if err != nil {
		return false
	}
	return strconv.FormatInt(rollout.GetObjectMeta().GetGeneration(), 10) == rollout.Status.ObservedGeneration &&
		rolloutCondition.Status == corev1.ConditionTrue
}

func completeCondition(rollout *v1alpha1.Rollout) (v1alpha1.RolloutCondition, error) {
	for i, condition := range rollout.Status.Conditions {
		if condition.Type == v1alpha1.RolloutCompleted {
			return rollout.Status.Conditions[i], nil
		}
	}
	return v1alpha1.RolloutCondition{}, errors.New("condition RolloutCompleted not found")
}

func rolloutAborted(rollout *v1alpha1.Rollout) bool {
	return rollout.Status.Abort
}

func getPluginConfig(rollout *v1alpha1.Rollout) (*ConsulTrafficRouting, error) {
	consulConfig := ConsulTrafficRouting{}
	if err := json.Unmarshal(rollout.Spec.Strategy.Canary.TrafficRouting.Plugins[ConfigKey], &consulConfig); err != nil {
		return nil, err
	}
	return &consulConfig, nil
}
