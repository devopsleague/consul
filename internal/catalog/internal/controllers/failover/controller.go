// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: BUSL-1.1

package failover

import (
	"context"

	"github.com/hashicorp/consul/internal/catalog/internal/indexers"
	"github.com/hashicorp/consul/internal/catalog/internal/types"
	"github.com/hashicorp/consul/internal/controller"
	"github.com/hashicorp/consul/internal/resource"
	pbcatalog "github.com/hashicorp/consul/proto-public/pbcatalog/v2beta1"
	"github.com/hashicorp/consul/proto-public/pbresource"
)

func FailoverPolicyController() controller.Controller {
	return controller.ForType(pbcatalog.FailoverPolicyType).
		WithIndex(pbcatalog.FailoverPolicyType, "destinations", indexers.FailoverDestinationsIndexer()).
		WithWatch(pbcatalog.ServiceType, controller.CacheListMapper(pbcatalog.FailoverPolicyType, "destinations")).
		WithReconciler(newFailoverPolicyReconciler())
}

type failoverPolicyReconciler struct{}

func newFailoverPolicyReconciler() *failoverPolicyReconciler {
	return &failoverPolicyReconciler{}
}

func (r *failoverPolicyReconciler) Reconcile(ctx context.Context, rt controller.Runtime, req controller.Request) error {
	// The runtime is passed by value so replacing it here for the remainder of this
	// reconciliation request processing will not affect future invocations.
	rt.Logger = rt.Logger.With("resource-id", req.ID, "controller", StatusKey)

	rt.Logger.Trace("reconciling failover policy")

	failoverPolicyID := req.ID

	failoverPolicy, err := getFailoverPolicy(ctx, rt, failoverPolicyID)
	if err != nil {
		rt.Logger.Error("error retrieving failover policy", "error", err)
		return err
	}

	// FailoverPolicy is name-aligned with the Service it controls.
	serviceID := &pbresource.ID{
		Type:    pbcatalog.ServiceType,
		Tenancy: failoverPolicyID.Tenancy,
		Name:    failoverPolicyID.Name,
	}

	service, err := getService(ctx, rt, serviceID)
	if err != nil {
		rt.Logger.Error("error retrieving corresponding service", "error", err)
		return err
	}
	destServices := make(map[resource.ReferenceKey]*resource.DecodedResource[*pbcatalog.Service])
	if service != nil {
		destServices[resource.NewReferenceKey(serviceID)] = service
	}

	// Denorm the ports and stuff. After this we have no empty ports.
	if service != nil {
		failoverPolicy.Data = types.SimplifyFailoverPolicy(
			service.Data,
			failoverPolicy.Data,
		)
	}

	// Fetch services.
	for _, dest := range failoverPolicy.Data.GetUnderlyingDestinations() {
		if dest.Ref == nil || !isServiceType(dest.Ref.Type) || dest.Ref.Section != "" {
			continue // invalid, not possible due to validation hook
		}

		key := resource.NewReferenceKey(dest.Ref)

		if _, ok := destServices[key]; ok {
			continue
		}

		destID := resource.IDFromReference(dest.Ref)

		destService, err := getService(ctx, rt, destID)
		if err != nil {
			rt.Logger.Error("error retrieving destination service", "service", key, "error", err)
			return err
		}

		if destService != nil {
			destServices[key] = destService
		}
	}

	newStatus := computeNewStatus(failoverPolicy, service, destServices)

	if resource.EqualStatus(failoverPolicy.Resource.Status[StatusKey], newStatus, false) {
		rt.Logger.Trace("resource's failover policy status is unchanged",
			"conditions", newStatus.Conditions)
		return nil
	}

	_, err = rt.Client.WriteStatus(ctx, &pbresource.WriteStatusRequest{
		Id:     failoverPolicy.Resource.Id,
		Key:    StatusKey,
		Status: newStatus,
	})

	if err != nil {
		rt.Logger.Error("error encountered when attempting to update the resource's failover policy status", "error", err)
		return err
	}

	rt.Logger.Trace("resource's failover policy status was updated",
		"conditions", newStatus.Conditions)
	return nil
}

func getFailoverPolicy(ctx context.Context, rt controller.Runtime, id *pbresource.ID) (*resource.DecodedResource[*pbcatalog.FailoverPolicy], error) {
	return resource.GetDecodedResource[*pbcatalog.FailoverPolicy](ctx, rt.Client, id)
}

func getService(ctx context.Context, rt controller.Runtime, id *pbresource.ID) (*resource.DecodedResource[*pbcatalog.Service], error) {
	return resource.GetDecodedResource[*pbcatalog.Service](ctx, rt.Client, id)
}

func computeNewStatus(
	failoverPolicy *resource.DecodedResource[*pbcatalog.FailoverPolicy],
	service *resource.DecodedResource[*pbcatalog.Service],
	destServices map[resource.ReferenceKey]*resource.DecodedResource[*pbcatalog.Service],
) *pbresource.Status {
	if service == nil {
		return &pbresource.Status{
			ObservedGeneration: failoverPolicy.Resource.Generation,
			Conditions: []*pbresource.Condition{
				ConditionMissingService,
			},
		}
	}

	allowedPortProtocols := make(map[string]pbcatalog.Protocol)
	for _, port := range service.Data.Ports {
		if port.Protocol == pbcatalog.Protocol_PROTOCOL_MESH {
			continue // skip
		}
		allowedPortProtocols[port.TargetPort] = port.Protocol
	}

	var conditions []*pbresource.Condition

	if failoverPolicy.Data.Config != nil {
		for _, dest := range failoverPolicy.Data.Config.Destinations {
			// We know from validation that a Ref must be set, and the type it
			// points to is a Service.
			//
			// Rather than do additional validation, just do a quick
			// belt-and-suspenders check-and-skip if something looks weird.
			if dest.Ref == nil || !isServiceType(dest.Ref.Type) {
				continue
			}

			if cond := serviceHasPort(dest, destServices); cond != nil {
				conditions = append(conditions, cond)
			}
		}
		// TODO: validate that referenced sameness groups exist
	}

	for port, pc := range failoverPolicy.Data.PortConfigs {
		if _, ok := allowedPortProtocols[port]; !ok {
			conditions = append(conditions, ConditionUnknownPort(port))
		}

		for _, dest := range pc.Destinations {
			// We know from validation that a Ref must be set, and the type it
			// points to is a Service.
			//
			// Rather than do additional validation, just do a quick
			// belt-and-suspenders check-and-skip if something looks weird.
			if dest.Ref == nil || !isServiceType(dest.Ref.Type) {
				continue
			}

			if cond := serviceHasPort(dest, destServices); cond != nil {
				conditions = append(conditions, cond)
			}
		}

		// TODO: validate that referenced sameness groups exist
	}

	if len(conditions) > 0 {
		return &pbresource.Status{
			ObservedGeneration: failoverPolicy.Resource.Generation,
			Conditions:         conditions,
		}
	}

	return &pbresource.Status{
		ObservedGeneration: failoverPolicy.Resource.Generation,
		Conditions: []*pbresource.Condition{
			ConditionOK,
		},
	}
}

func serviceHasPort(
	dest *pbcatalog.FailoverDestination,
	destServices map[resource.ReferenceKey]*resource.DecodedResource[*pbcatalog.Service],
) *pbresource.Condition {
	key := resource.NewReferenceKey(dest.Ref)
	destService, ok := destServices[key]
	if !ok {
		return ConditionMissingDestinationService(dest.Ref)
	}

	found := false
	mesh := false
	for _, port := range destService.Data.Ports {
		if port.TargetPort == dest.Port {
			found = true
			if port.Protocol == pbcatalog.Protocol_PROTOCOL_MESH {
				mesh = true
			}
			break
		}
	}

	if !found {
		return ConditionUnknownDestinationPort(dest.Ref, dest.Port)
	} else if mesh {
		return ConditionUsingMeshDestinationPort(dest.Ref, dest.Port)
	}

	return nil
}

func isServiceType(typ *pbresource.Type) bool {
	switch {
	case resource.EqualType(typ, pbcatalog.ServiceType):
		return true
	}
	return false
}
