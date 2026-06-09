package instance

import (
	"testing"

	spdkapi "github.com/longhorn/longhorn-spdk-engine/pkg/api"

	"github.com/longhorn/longhorn-instance-manager/pkg/types"
)

// instanceStateRunning is the SPDK-reported state string for a running
// instance (spdk-engine types.InstanceStateRunning); the conversion helpers
// pass it through verbatim and the manager compares against the equivalent
// longhorn.InstanceStateRunning to drive the Running transition.
const instanceStateRunning = "running"

// The manager's instance-manager monitor classifies every instance returned by
// InstanceList by its Spec.Type into the per-type maps on
// InstanceManager.Status (InstanceEngines / InstanceEngineFrontends /
// InstanceReplicas). The reconcile loop then only observes an instance as
// Running once it appears in the map for its type. So each conversion helper
// MUST stamp the correct InstanceType and pass through the SPDK-reported state;
// otherwise the corresponding CR (engine / engine-frontend / replica) never
// leaves Starting and its volume hangs in attaching.
//
// This is the contract that regressed when V2DataEngineInstanceOps.InstanceList
// listed replicas and engines but omitted engine frontends entirely: the helper
// existed and stamped the right type, but nothing called it, so engine
// frontends were never enumerated. These tests pin the per-type stamping that
// makes the enumeration meaningful.
func TestEngineFrontendResponseToInstanceResponse(t *testing.T) {
	ef := &spdkapi.EngineFrontend{
		Name:       "pvc-test-ef-0",
		EngineName: "pvc-test-e-0",
		Endpoint:   "/dev/longhorn/pvc-test",
		Frontend:   "spdk-tcp-blockdev",
		State:      instanceStateRunning,
	}

	got := engineFrontendResponseToInstanceResponse(ef)

	if got.Spec.Type != types.InstanceTypeEngineFrontend {
		t.Errorf("type: got %q, want %q", got.Spec.Type, types.InstanceTypeEngineFrontend)
	}
	if got.Spec.Name != ef.Name {
		t.Errorf("name: got %q, want %q", got.Spec.Name, ef.Name)
	}
	if got.Status.State != ef.State {
		t.Errorf("state: got %q, want %q (manager keys the Running transition on this)", got.Status.State, ef.State)
	}
	if got.Status.EngineName != ef.EngineName {
		t.Errorf("engineName: got %q, want %q", got.Status.EngineName, ef.EngineName)
	}
	if got.Status.Endpoint != ef.Endpoint {
		t.Errorf("endpoint: got %q, want %q", got.Status.Endpoint, ef.Endpoint)
	}
}

func TestEngineResponseToInstanceResponseType(t *testing.T) {
	got := engineResponseToInstanceResponse(&spdkapi.Engine{Name: "pvc-test-e-0", State: instanceStateRunning})
	if got.Spec.Type != types.InstanceTypeEngine {
		t.Errorf("type: got %q, want %q", got.Spec.Type, types.InstanceTypeEngine)
	}
	if got.Status.State != instanceStateRunning {
		t.Errorf("state: got %q, want %q", got.Status.State, instanceStateRunning)
	}
}

func TestReplicaResponseToInstanceResponseType(t *testing.T) {
	got := replicaResponseToInstanceResponse(&spdkapi.Replica{Name: "pvc-test-r-0", State: instanceStateRunning})
	if got.Spec.Type != types.InstanceTypeReplica {
		t.Errorf("type: got %q, want %q", got.Spec.Type, types.InstanceTypeReplica)
	}
	if got.Status.State != instanceStateRunning {
		t.Errorf("state: got %q, want %q", got.Status.State, instanceStateRunning)
	}
}
