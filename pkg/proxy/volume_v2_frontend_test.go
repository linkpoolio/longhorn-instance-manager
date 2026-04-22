package proxy

import (
	"errors"
	"fmt"
	"testing"

	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	"github.com/longhorn/longhorn-spdk-engine/pkg/api"

	enginerpc "github.com/longhorn/types/pkg/generated/enginerpc"
	rpc "github.com/longhorn/types/pkg/generated/imrpc"
)

// fakeV2FrontendClient records calls and returns configured responses so
// unit tests can exercise the proxy's V2 frontend helpers without a live
// SPDK server.
type fakeV2FrontendClient struct {
	engines   map[string]*api.Engine
	frontends map[string]*api.EngineFrontend

	engineGetErr       error
	frontendGetErr     error
	frontendCreateErr  error
	frontendDeleteErr  error

	createCalls []engineFrontendCreateCall
	deleteCalls []string
}

type engineFrontendCreateCall struct {
	Name              string
	VolumeName        string
	EngineName        string
	Frontend          string
	SpecSize          uint64
	TargetAddress     string
	UblkQueueDepth    int32
	UblkNumberOfQueue int32
}

func (f *fakeV2FrontendClient) EngineGet(name string) (*api.Engine, error) {
	if f.engineGetErr != nil {
		return nil, f.engineGetErr
	}
	e, ok := f.engines[name]
	if !ok {
		return nil, fmt.Errorf("engine %q not found", name)
	}
	return e, nil
}

func (f *fakeV2FrontendClient) EngineFrontendGet(name string) (*api.EngineFrontend, error) {
	if f.frontendGetErr != nil {
		return nil, f.frontendGetErr
	}
	ef, ok := f.frontends[name]
	if !ok {
		return nil, fmt.Errorf("engine frontend %q not found", name)
	}
	return ef, nil
}

func (f *fakeV2FrontendClient) EngineFrontendCreate(name, volumeName, engineName, frontend string, specSize uint64, targetAddress string,
	ublkQueueDepth, ublkNumberOfQueue int32) (*api.EngineFrontend, error) {
	f.createCalls = append(f.createCalls, engineFrontendCreateCall{
		Name: name, VolumeName: volumeName, EngineName: engineName, Frontend: frontend,
		SpecSize: specSize, TargetAddress: targetAddress,
		UblkQueueDepth: ublkQueueDepth, UblkNumberOfQueue: ublkNumberOfQueue,
	})
	if f.frontendCreateErr != nil {
		return nil, f.frontendCreateErr
	}
	ef := &api.EngineFrontend{Name: name, VolumeName: volumeName, EngineName: engineName, Frontend: frontend, SpecSize: specSize}
	if f.frontends == nil {
		f.frontends = map[string]*api.EngineFrontend{}
	}
	f.frontends[name] = ef
	return ef, nil
}

func (f *fakeV2FrontendClient) EngineFrontendDelete(name string) error {
	f.deleteCalls = append(f.deleteCalls, name)
	if f.frontendDeleteErr != nil {
		return f.frontendDeleteErr
	}
	delete(f.frontends, name)
	return nil
}

func newStartRequest(engineName, volumeName, frontend, address string) *rpc.EngineVolumeFrontendStartRequest {
	return &rpc.EngineVolumeFrontendStartRequest{
		ProxyEngineRequest: &rpc.ProxyEngineRequest{
			EngineName: engineName,
			VolumeName: volumeName,
			Address:    address,
			DataEngine: rpc.DataEngine_DATA_ENGINE_V2,
		},
		FrontendStart: &enginerpc.VolumeFrontendStartRequest{
			Frontend: frontend,
		},
	}
}

func TestStartV2EngineFrontend_HappyPath(t *testing.T) {
	const specSize uint64 = 5 * 1024 * 1024 * 1024
	f := &fakeV2FrontendClient{
		engines: map[string]*api.Engine{
			"eng-0": {Name: "eng-0", VolumeName: "vol", SpecSize: specSize},
		},
	}
	req := newStartRequest("eng-0", "vol", "spdk-tcp-blockdev", "10.10.4.11:20001")

	if err := startV2EngineFrontend(f, req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.createCalls) != 1 {
		t.Fatalf("expected 1 EngineFrontendCreate call, got %d", len(f.createCalls))
	}
	got := f.createCalls[0]
	want := engineFrontendCreateCall{
		Name:          "eng-0",
		VolumeName:    "vol",
		EngineName:    "eng-0",
		Frontend:      "spdk-tcp-blockdev",
		SpecSize:      specSize,
		TargetAddress: "10.10.4.11:20001",
	}
	if got != want {
		t.Fatalf("EngineFrontendCreate args mismatch\n got: %+v\nwant: %+v", got, want)
	}
}

func TestStartV2EngineFrontend_AlreadyExistsIsIdempotent(t *testing.T) {
	f := &fakeV2FrontendClient{
		engines: map[string]*api.Engine{
			"eng-0": {SpecSize: 1024},
		},
		frontendCreateErr: grpcstatus.Errorf(grpccodes.AlreadyExists, "frontend eng-0 already exists"),
	}
	req := newStartRequest("eng-0", "vol", "spdk-tcp-blockdev", "10.10.4.11:20001")

	if err := startV2EngineFrontend(f, req); err != nil {
		t.Fatalf("AlreadyExists must be treated as success, got: %v", err)
	}
}

func TestStartV2EngineFrontend_EngineGetError(t *testing.T) {
	f := &fakeV2FrontendClient{engineGetErr: errors.New("spdk unreachable")}
	req := newStartRequest("eng-0", "vol", "spdk-tcp-blockdev", "10.10.4.11:20001")

	err := startV2EngineFrontend(f, req)
	if err == nil {
		t.Fatal("expected error when EngineGet fails")
	}
	if len(f.createCalls) != 0 {
		t.Fatalf("EngineFrontendCreate must not be called after EngineGet failure")
	}
}

func TestStartV2EngineFrontend_CreatePropagatesNonAlreadyExistsError(t *testing.T) {
	f := &fakeV2FrontendClient{
		engines:           map[string]*api.Engine{"eng-0": {SpecSize: 1024}},
		frontendCreateErr: grpcstatus.Errorf(grpccodes.Internal, "boom"),
	}
	req := newStartRequest("eng-0", "vol", "spdk-tcp-blockdev", "10.10.4.11:20001")

	err := startV2EngineFrontend(f, req)
	if err == nil {
		t.Fatal("expected error to propagate for non-AlreadyExists failure")
	}
	if grpcstatus.Code(err) != grpccodes.Internal {
		t.Fatalf("expected gRPC Internal error, got code %v", grpcstatus.Code(err))
	}
}

func TestShutdownV2EngineFrontend_DefaultNameIsEngineName(t *testing.T) {
	f := &fakeV2FrontendClient{
		frontends: map[string]*api.EngineFrontend{"eng-0": {Name: "eng-0"}},
	}
	req := &rpc.ProxyEngineRequest{EngineName: "eng-0"}

	if err := shutdownV2EngineFrontend(f, req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.deleteCalls) != 1 || f.deleteCalls[0] != "eng-0" {
		t.Fatalf("expected delete of 'eng-0', got %v", f.deleteCalls)
	}
}

func TestShutdownV2EngineFrontend_ExplicitFrontendNameWins(t *testing.T) {
	f := &fakeV2FrontendClient{}
	req := &rpc.ProxyEngineRequest{EngineName: "eng-0", EngineFrontendName: "eng-0-fe"}

	if err := shutdownV2EngineFrontend(f, req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.deleteCalls) != 1 || f.deleteCalls[0] != "eng-0-fe" {
		t.Fatalf("expected delete of 'eng-0-fe', got %v", f.deleteCalls)
	}
}

func TestShutdownV2EngineFrontend_NotFoundIsIdempotent(t *testing.T) {
	f := &fakeV2FrontendClient{
		frontendDeleteErr: grpcstatus.Errorf(grpccodes.NotFound, "frontend not found"),
	}
	req := &rpc.ProxyEngineRequest{EngineName: "eng-0"}

	if err := shutdownV2EngineFrontend(f, req); err != nil {
		t.Fatalf("NotFound must be treated as success, got: %v", err)
	}
}

func TestShutdownV2EngineFrontend_PropagatesNonNotFoundError(t *testing.T) {
	f := &fakeV2FrontendClient{
		frontendDeleteErr: grpcstatus.Errorf(grpccodes.Internal, "boom"),
	}
	req := &rpc.ProxyEngineRequest{EngineName: "eng-0"}

	if err := shutdownV2EngineFrontend(f, req); err == nil {
		t.Fatal("expected non-NotFound error to propagate")
	}
}

func TestResolveV2VolumeView_FrontendOverridesEngine(t *testing.T) {
	// Engine has an empty Endpoint (no frontend attached to it directly).
	// Frontend has populated Endpoint/Size/Frontend fields — those must
	// override the engine's view.
	f := &fakeV2FrontendClient{
		engines: map[string]*api.Engine{
			"eng-0": {
				Name: "eng-0", SpecSize: 100, Endpoint: "", Frontend: "",
				ReplicaAddressMap: map[string]string{"r-0": "a:1"},
			},
		},
		frontends: map[string]*api.EngineFrontend{
			"eng-0": {
				Name: "eng-0", SpecSize: 200, Endpoint: "/dev/nvme0n1",
				Frontend: "spdk-tcp-blockdev",
			},
		},
	}
	view, err := resolveV2VolumeView(f, "eng-0", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if view.Size != 200 {
		t.Errorf("Size should come from frontend, got %d", view.Size)
	}
	if view.Endpoint != "/dev/nvme0n1" {
		t.Errorf("Endpoint should come from frontend, got %q", view.Endpoint)
	}
	if view.Frontend != "spdk-tcp-blockdev" {
		t.Errorf("Frontend type should come from frontend resource, got %q", view.Frontend)
	}
}

func TestResolveV2VolumeView_FallsBackToEngineWhenFrontendMissing(t *testing.T) {
	// When EngineFrontendGet returns an error (e.g. the frontend hasn't been
	// created yet because VolumeFrontendStart has not landed), the view must
	// fall back to the engine's fields rather than erroring out — the
	// manager's refresh loop needs to keep calling VolumeFrontendStart.
	f := &fakeV2FrontendClient{
		engines: map[string]*api.Engine{
			"eng-0": {
				Name: "eng-0", SpecSize: 100, Endpoint: "", Frontend: "spdk-tcp-blockdev",
			},
		},
	}
	view, err := resolveV2VolumeView(f, "eng-0", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if view.Endpoint != "" {
		t.Errorf("Endpoint should be empty when frontend missing, got %q", view.Endpoint)
	}
	if view.Size != 100 {
		t.Errorf("Size should come from engine when frontend missing, got %d", view.Size)
	}
}

func TestResolveV2VolumeView_ExplicitEngineFrontendName(t *testing.T) {
	// When the manager passes an explicit EngineFrontendName, that name
	// is used for the lookup instead of the engine name.
	f := &fakeV2FrontendClient{
		engines: map[string]*api.Engine{
			"eng-0": {Name: "eng-0", SpecSize: 100},
		},
		frontends: map[string]*api.EngineFrontend{
			"custom-fe": {Name: "custom-fe", SpecSize: 300, Endpoint: "/dev/nvme9n1"},
		},
	}
	view, err := resolveV2VolumeView(f, "eng-0", "custom-fe")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if view.Endpoint != "/dev/nvme9n1" {
		t.Errorf("Endpoint should come from explicit frontend name, got %q", view.Endpoint)
	}
}

func TestResolveV2VolumeView_EngineGetErrorPropagates(t *testing.T) {
	f := &fakeV2FrontendClient{engineGetErr: errors.New("spdk unreachable")}
	if _, err := resolveV2VolumeView(f, "eng-0", ""); err == nil {
		t.Fatal("expected EngineGet error to propagate")
	}
}

func TestResolveV2VolumeView_IsExpandingIsOR(t *testing.T) {
	// IsExpanding reflects either engine or frontend expanding — so an
	// expansion reported by the engine alone still surfaces through the view.
	f := &fakeV2FrontendClient{
		engines: map[string]*api.Engine{
			"eng-0": {SpecSize: 100, IsExpanding: true},
		},
		frontends: map[string]*api.EngineFrontend{
			"eng-0": {Name: "eng-0", SpecSize: 200, IsExpanding: false},
		},
	}
	view, err := resolveV2VolumeView(f, "eng-0", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !view.IsExpanding {
		t.Error("IsExpanding should be OR-merged across engine and frontend")
	}
}

// Keep the test package buildable even when individual tests change.
var _ = testing.M{}
