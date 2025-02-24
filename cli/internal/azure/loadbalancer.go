/*
Copyright (c) Edgeless Systems GmbH

SPDX-License-Identifier: AGPL-3.0-only
*/

package azure

import (
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork"
	"github.com/edgelesssys/constellation/internal/constants"
)

// LoadBalancer defines a Azure load balancer.
type LoadBalancer struct {
	Name          string
	Subscription  string
	ResourceGroup string
	Location      string
	PublicIPID    string
	UID           string
}

const (
	BackendAddressPoolWorkerName       = "backendAddressWorkerPool"
	BackendAddressPoolControlPlaneName = "backendAddressControlPlanePool"
	frontEndIPConfigName               = "frontEndIPConfig"
	kubeHealthProbeName                = "kubeHealthProbe"
	verifyHealthProbeName              = "verifyHealthProbe"
	coordHealthProbeName               = "coordHealthProbe"
	debugdHealthProbeName              = "debugdHealthProbe"
	konnectivityHealthProbeName        = "konnectivityHealthProbe"
	recoveryHealthProbeName            = "recoveryHealthProbe"
)

// Azure returns a Azure representation of LoadBalancer.
func (l LoadBalancer) Azure() armnetwork.LoadBalancer {
	backEndAddressPoolNodeName := BackendAddressPoolWorkerName + "-" + l.UID
	backEndAddressPoolControlPlaneName := BackendAddressPoolControlPlaneName + "-" + l.UID

	return armnetwork.LoadBalancer{
		Name:     to.Ptr(l.Name),
		Location: to.Ptr(l.Location),
		SKU:      &armnetwork.LoadBalancerSKU{Name: to.Ptr(armnetwork.LoadBalancerSKUNameStandard)},
		Properties: &armnetwork.LoadBalancerPropertiesFormat{
			FrontendIPConfigurations: []*armnetwork.FrontendIPConfiguration{
				{
					Name: to.Ptr(frontEndIPConfigName),
					Properties: &armnetwork.FrontendIPConfigurationPropertiesFormat{
						PublicIPAddress: &armnetwork.PublicIPAddress{
							ID: to.Ptr(l.PublicIPID),
						},
					},
				},
			},
			BackendAddressPools: []*armnetwork.BackendAddressPool{
				{Name: to.Ptr(backEndAddressPoolNodeName)},
				{Name: to.Ptr(backEndAddressPoolControlPlaneName)},
				{Name: to.Ptr("all")},
			},
			Probes: []*armnetwork.Probe{
				{
					Name: to.Ptr(kubeHealthProbeName),
					Properties: &armnetwork.ProbePropertiesFormat{
						Protocol: to.Ptr(armnetwork.ProbeProtocolTCP),
						Port:     to.Ptr(int32(constants.KubernetesPort)),
					},
				},
				{
					Name: to.Ptr(verifyHealthProbeName),
					Properties: &armnetwork.ProbePropertiesFormat{
						Protocol: to.Ptr(armnetwork.ProbeProtocolTCP),
						Port:     to.Ptr[int32](constants.VerifyServiceNodePortGRPC),
					},
				},
				{
					Name: to.Ptr(coordHealthProbeName),
					Properties: &armnetwork.ProbePropertiesFormat{
						Protocol: to.Ptr(armnetwork.ProbeProtocolTCP),
						Port:     to.Ptr[int32](constants.BootstrapperPort),
					},
				},
				{
					Name: to.Ptr(debugdHealthProbeName),
					Properties: &armnetwork.ProbePropertiesFormat{
						Protocol: to.Ptr(armnetwork.ProbeProtocolTCP),
						Port:     to.Ptr[int32](constants.DebugdPort),
					},
				},
				{
					Name: to.Ptr(konnectivityHealthProbeName),
					Properties: &armnetwork.ProbePropertiesFormat{
						Protocol: to.Ptr(armnetwork.ProbeProtocolTCP),
						Port:     to.Ptr[int32](constants.KonnectivityPort),
					},
				},
				{
					Name: to.Ptr(recoveryHealthProbeName),
					Properties: &armnetwork.ProbePropertiesFormat{
						Protocol: to.Ptr(armnetwork.ProbeProtocolTCP),
						Port:     to.Ptr[int32](constants.RecoveryPort),
					},
				},
			},
			LoadBalancingRules: []*armnetwork.LoadBalancingRule{
				{
					Name: to.Ptr("kubeLoadBalancerRule"),
					Properties: &armnetwork.LoadBalancingRulePropertiesFormat{
						FrontendIPConfiguration: &armnetwork.SubResource{
							ID: to.Ptr("/subscriptions/" + l.Subscription +
								"/resourceGroups/" + l.ResourceGroup +
								"/providers/Microsoft.Network/loadBalancers/" + l.Name +
								"/frontendIPConfigurations/" + frontEndIPConfigName),
						},
						FrontendPort: to.Ptr[int32](constants.KubernetesPort),
						BackendPort:  to.Ptr[int32](constants.KubernetesPort),
						Protocol:     to.Ptr(armnetwork.TransportProtocolTCP),
						Probe: &armnetwork.SubResource{
							ID: to.Ptr("/subscriptions/" + l.Subscription +
								"/resourceGroups/" + l.ResourceGroup +
								"/providers/Microsoft.Network/loadBalancers/" + l.Name +
								"/probes/" + kubeHealthProbeName),
						},
						DisableOutboundSnat: to.Ptr(true),
						BackendAddressPools: []*armnetwork.SubResource{
							{
								ID: to.Ptr("/subscriptions/" + l.Subscription +
									"/resourceGroups/" + l.ResourceGroup +
									"/providers/Microsoft.Network/loadBalancers/" + l.Name +
									"/backendAddressPools/" + backEndAddressPoolControlPlaneName),
							},
						},
					},
				},
				{
					Name: to.Ptr("verifyLoadBalancerRule"),
					Properties: &armnetwork.LoadBalancingRulePropertiesFormat{
						FrontendIPConfiguration: &armnetwork.SubResource{
							ID: to.Ptr("/subscriptions/" + l.Subscription +
								"/resourceGroups/" + l.ResourceGroup +
								"/providers/Microsoft.Network/loadBalancers/" + l.Name +
								"/frontendIPConfigurations/" + frontEndIPConfigName),
						},
						FrontendPort: to.Ptr[int32](constants.VerifyServiceNodePortGRPC),
						BackendPort:  to.Ptr[int32](constants.VerifyServiceNodePortGRPC),
						Protocol:     to.Ptr(armnetwork.TransportProtocolTCP),
						Probe: &armnetwork.SubResource{
							ID: to.Ptr("/subscriptions/" + l.Subscription +
								"/resourceGroups/" + l.ResourceGroup +
								"/providers/Microsoft.Network/loadBalancers/" + l.Name +
								"/probes/" + verifyHealthProbeName),
						},
						DisableOutboundSnat: to.Ptr(true),
						BackendAddressPools: []*armnetwork.SubResource{
							{
								ID: to.Ptr("/subscriptions/" + l.Subscription +
									"/resourceGroups/" + l.ResourceGroup +
									"/providers/Microsoft.Network/loadBalancers/" + l.Name +
									"/backendAddressPools/" + backEndAddressPoolControlPlaneName),
							},
						},
					},
				},
				{
					Name: to.Ptr("coordLoadBalancerRule"),
					Properties: &armnetwork.LoadBalancingRulePropertiesFormat{
						FrontendIPConfiguration: &armnetwork.SubResource{
							ID: to.Ptr("/subscriptions/" + l.Subscription +
								"/resourceGroups/" + l.ResourceGroup +
								"/providers/Microsoft.Network/loadBalancers/" + l.Name +
								"/frontendIPConfigurations/" + frontEndIPConfigName),
						},
						FrontendPort: to.Ptr[int32](constants.BootstrapperPort),
						BackendPort:  to.Ptr[int32](constants.BootstrapperPort),
						Protocol:     to.Ptr(armnetwork.TransportProtocolTCP),
						Probe: &armnetwork.SubResource{
							ID: to.Ptr("/subscriptions/" + l.Subscription +
								"/resourceGroups/" + l.ResourceGroup +
								"/providers/Microsoft.Network/loadBalancers/" + l.Name +
								"/probes/" + coordHealthProbeName),
						},
						DisableOutboundSnat: to.Ptr(true),
						BackendAddressPools: []*armnetwork.SubResource{
							{
								ID: to.Ptr("/subscriptions/" + l.Subscription +
									"/resourceGroups/" + l.ResourceGroup +
									"/providers/Microsoft.Network/loadBalancers/" + l.Name +
									"/backendAddressPools/" + backEndAddressPoolControlPlaneName),
							},
						},
					},
				},
				{
					Name: to.Ptr("konnectivityLoadBalancerRule"),
					Properties: &armnetwork.LoadBalancingRulePropertiesFormat{
						FrontendIPConfiguration: &armnetwork.SubResource{
							ID: to.Ptr("/subscriptions/" + l.Subscription +
								"/resourceGroups/" + l.ResourceGroup +
								"/providers/Microsoft.Network/loadBalancers/" + l.Name +
								"/frontendIPConfigurations/" + frontEndIPConfigName),
						},
						FrontendPort: to.Ptr[int32](constants.KonnectivityPort),
						BackendPort:  to.Ptr[int32](constants.KonnectivityPort),
						Protocol:     to.Ptr(armnetwork.TransportProtocolTCP),
						Probe: &armnetwork.SubResource{
							ID: to.Ptr("/subscriptions/" + l.Subscription +
								"/resourceGroups/" + l.ResourceGroup +
								"/providers/Microsoft.Network/loadBalancers/" + l.Name +
								"/probes/" + konnectivityHealthProbeName),
						},
						DisableOutboundSnat: to.Ptr(true),
						BackendAddressPools: []*armnetwork.SubResource{
							{
								ID: to.Ptr("/subscriptions/" + l.Subscription +
									"/resourceGroups/" + l.ResourceGroup +
									"/providers/Microsoft.Network/loadBalancers/" + l.Name +
									"/backendAddressPools/" + backEndAddressPoolControlPlaneName),
							},
						},
					},
				},
				{
					Name: to.Ptr("recoveryLoadBalancerRule"),
					Properties: &armnetwork.LoadBalancingRulePropertiesFormat{
						FrontendIPConfiguration: &armnetwork.SubResource{
							ID: to.Ptr("/subscriptions/" + l.Subscription +
								"/resourceGroups/" + l.ResourceGroup +
								"/providers/Microsoft.Network/loadBalancers/" + l.Name +
								"/frontendIPConfigurations/" + frontEndIPConfigName),
						},
						FrontendPort: to.Ptr[int32](constants.RecoveryPort),
						BackendPort:  to.Ptr[int32](constants.RecoveryPort),
						Protocol:     to.Ptr(armnetwork.TransportProtocolTCP),
						Probe: &armnetwork.SubResource{
							ID: to.Ptr("/subscriptions/" + l.Subscription +
								"/resourceGroups/" + l.ResourceGroup +
								"/providers/Microsoft.Network/loadBalancers/" + l.Name +
								"/probes/" + recoveryHealthProbeName),
						},
						DisableOutboundSnat: to.Ptr(true),
						BackendAddressPools: []*armnetwork.SubResource{
							{
								ID: to.Ptr("/subscriptions/" + l.Subscription +
									"/resourceGroups/" + l.ResourceGroup +
									"/providers/Microsoft.Network/loadBalancers/" + l.Name +
									"/backendAddressPools/" + backEndAddressPoolControlPlaneName),
							},
						},
					},
				},
			},
			OutboundRules: []*armnetwork.OutboundRule{
				{
					Name: to.Ptr("outboundRuleControlPlane"),
					Properties: &armnetwork.OutboundRulePropertiesFormat{
						FrontendIPConfigurations: []*armnetwork.SubResource{
							{
								ID: to.Ptr("/subscriptions/" + l.Subscription +
									"/resourceGroups/" + l.ResourceGroup +
									"/providers/Microsoft.Network/loadBalancers/" + l.Name +
									"/frontendIPConfigurations/" + frontEndIPConfigName),
							},
						},
						BackendAddressPool: &armnetwork.SubResource{
							ID: to.Ptr("/subscriptions/" + l.Subscription +
								"/resourceGroups/" + l.ResourceGroup +
								"/providers/Microsoft.Network/loadBalancers/" + l.Name +
								"/backendAddressPools/all"),
						},
						Protocol: to.Ptr(armnetwork.LoadBalancerOutboundRuleProtocolAll),
					},
				},
			},
		},
	}
}

func (l *LoadBalancer) AppendDebugRules(armLoadBalancer armnetwork.LoadBalancer) armnetwork.LoadBalancer {
	backEndAddressPoolControlPlaneName := BackendAddressPoolControlPlaneName + "-" + l.UID

	if armLoadBalancer.Properties == nil {
		armLoadBalancer.Properties = &armnetwork.LoadBalancerPropertiesFormat{}
	}

	if armLoadBalancer.Properties.LoadBalancingRules == nil {
		armLoadBalancer.Properties.LoadBalancingRules = []*armnetwork.LoadBalancingRule{}
	}

	debugdRule := armnetwork.LoadBalancingRule{
		Name: to.Ptr("debugdLoadBalancerRule"),
		Properties: &armnetwork.LoadBalancingRulePropertiesFormat{
			FrontendIPConfiguration: &armnetwork.SubResource{
				ID: to.Ptr("/subscriptions/" + l.Subscription +
					"/resourceGroups/" + l.ResourceGroup +
					"/providers/Microsoft.Network/loadBalancers/" + l.Name +
					"/frontendIPConfigurations/" + frontEndIPConfigName),
			},
			FrontendPort: to.Ptr[int32](constants.DebugdPort),
			BackendPort:  to.Ptr[int32](constants.DebugdPort),
			Protocol:     to.Ptr(armnetwork.TransportProtocolTCP),
			Probe: &armnetwork.SubResource{
				ID: to.Ptr("/subscriptions/" + l.Subscription +
					"/resourceGroups/" + l.ResourceGroup +
					"/providers/Microsoft.Network/loadBalancers/" + l.Name +
					"/probes/" + debugdHealthProbeName),
			},
			DisableOutboundSnat: to.Ptr(true),
			BackendAddressPools: []*armnetwork.SubResource{
				{
					ID: to.Ptr("/subscriptions/" + l.Subscription +
						"/resourceGroups/" + l.ResourceGroup +
						"/providers/Microsoft.Network/loadBalancers/" + l.Name +
						"/backendAddressPools/" + backEndAddressPoolControlPlaneName),
				},
			},
		},
	}

	armLoadBalancer.Properties.LoadBalancingRules = append(armLoadBalancer.Properties.LoadBalancingRules, &debugdRule)

	return armLoadBalancer
}
