package proxy

import (
	"net"
	"strconv"

	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	"github.com/longhorn/longhorn-spdk-engine/pkg/api"

	rpc "github.com/longhorn/types/pkg/generated/imrpc"
)

// v2FrontendSPDKClient is the minimal SPDK gRPC surface the V2 proxy
// frontend shims need. Extracted as an interface so unit tests can stub
// it without spinning up a real SPDK target.
type v2FrontendSPDKClient interface {
	EngineGet(name string) (*api.Engine, error)
	EngineFrontendGet(name string) (*api.EngineFrontend, error)
	EngineFrontendCreate(name, volumeName, engineName, frontend string, specSize uint64, targetAddress string,
		ublkQueueDepth, ublkNumberOfQueue int32) (*api.EngineFrontend, error)
	EngineFrontendDelete(name string) error
}

// startV2EngineFrontend translates a legacy VolumeFrontendStart request into
// the new EngineFrontendCreate RPC. One frontend per engine, reusing the
// engine name; AlreadyExists is treated as success so retries from
// longhorn-manager's refresh loop are idempotent.
func startV2EngineFrontend(client v2FrontendSPDKClient, req *rpc.EngineVolumeFrontendStartRequest) error {
	engine, err := client.EngineGet(req.ProxyEngineRequest.EngineName)
	if err != nil {
		return grpcstatus.Errorf(grpccodes.Internal, "failed to get engine %v for frontend start: %v", req.ProxyEngineRequest.EngineName, err)
	}

	// If a frontend already exists from a previous engine instance, its target
	// address can point at a port the new engine doesn't own anymore (ports
	// are reallocated on engine recreate). The kernel initiator then retries
	// forever with "connection refused". Compare and delete-recreate on
	// mismatch rather than returning AlreadyExists as idempotent success.
	if existing, getErr := client.EngineFrontendGet(req.ProxyEngineRequest.EngineName); getErr == nil && existing != nil {
		if engineFrontendTargetMatches(existing, req.ProxyEngineRequest.Address) {
			return nil
		}
		if err := client.EngineFrontendDelete(req.ProxyEngineRequest.EngineName); err != nil && grpcstatus.Code(err) != grpccodes.NotFound {
			return grpcstatus.Errorf(grpccodes.Internal, "failed to delete stale engine frontend %v before recreate: %v", req.ProxyEngineRequest.EngineName, err)
		}
	}

	_, err = client.EngineFrontendCreate(
		req.ProxyEngineRequest.EngineName,
		req.ProxyEngineRequest.VolumeName,
		req.ProxyEngineRequest.EngineName,
		req.FrontendStart.Frontend,
		engine.SpecSize,
		req.ProxyEngineRequest.Address,
		0, 0,
	)
	if err != nil && grpcstatus.Code(err) != grpccodes.AlreadyExists {
		return err
	}
	return nil
}

// engineFrontendTargetMatches returns true if the frontend's recorded target
// address equals the address we'd pass to EngineFrontendCreate. The address
// fed into VolumeFrontendStart is an ip:port string; EngineFrontend stores
// TargetIP/TargetPort separately.
func engineFrontendTargetMatches(ef *api.EngineFrontend, address string) bool {
	if ef == nil {
		return false
	}
	return net.JoinHostPort(ef.TargetIP, strconv.Itoa(int(ef.TargetPort))) == address
}

// shutdownV2EngineFrontend mirrors startV2EngineFrontend: delete the
// EngineFrontend named after the engine (falling back from an explicit
// EngineFrontendName when provided). NotFound is treated as success so
// repeated shutdowns are idempotent.
func shutdownV2EngineFrontend(client v2FrontendSPDKClient, req *rpc.ProxyEngineRequest) error {
	name := req.EngineFrontendName
	if name == "" {
		name = req.EngineName
	}
	if err := client.EngineFrontendDelete(name); err != nil && grpcstatus.Code(err) != grpccodes.NotFound {
		return err
	}
	return nil
}

// v2VolumeView holds the fields the proxy's VolumeGet overlays on top of
// the Engine. When an EngineFrontend exists, its view takes precedence so
// the manager sees the block-device endpoint rather than the engine's
// (usually empty) endpoint.
type v2VolumeView struct {
	Size                  int64
	Endpoint              string
	Frontend              string
	IsExpanding           bool
	LastExpansionError    string
	LastExpansionFailedAt string
}

// resolveV2VolumeView returns the volume view for VolumeGet. If the manager
// passes an explicit EngineFrontendName, that frontend is queried; otherwise
// the engine name is used as a fallback because startV2EngineFrontend creates
// the frontend with name == engineName. Missing or failed frontend lookups
// fall back to the engine's own fields rather than propagating the error.
func resolveV2VolumeView(client v2FrontendSPDKClient, engineName, engineFrontendName string) (*v2VolumeView, error) {
	engine, err := client.EngineGet(engineName)
	if err != nil {
		return nil, err
	}

	view := &v2VolumeView{
		Size:                  int64(engine.SpecSize),
		Endpoint:              engine.Endpoint,
		Frontend:              engine.Frontend,
		IsExpanding:           engine.IsExpanding,
		LastExpansionError:    engine.LastExpansionError,
		LastExpansionFailedAt: engine.LastExpansionFailedAt,
	}

	lookup := engineFrontendName
	if lookup == "" {
		lookup = engineName
	}
	if lookup == "" {
		return view, nil
	}

	frontend, err := client.EngineFrontendGet(lookup)
	if err != nil || frontend == nil {
		return view, nil
	}

	view.Size = int64(frontend.SpecSize)
	view.Endpoint = frontend.Endpoint
	view.Frontend = frontend.Frontend
	view.IsExpanding = view.IsExpanding || frontend.IsExpanding
	if frontend.LastExpansionError != "" {
		view.LastExpansionError = frontend.LastExpansionError
		view.LastExpansionFailedAt = frontend.LastExpansionFailedAt
	}
	return view, nil
}
