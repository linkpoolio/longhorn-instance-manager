package instance

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"reflect"
	"testing"
	"time"

	spdkapi "github.com/longhorn/longhorn-spdk-engine/pkg/api"
	rpc "github.com/longhorn/types/pkg/generated/imrpc"

	"github.com/longhorn/longhorn-instance-manager/pkg/types"
)

func buildTestTLSConfig(t *testing.T) *tls.Config {
	t.Helper()
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create CA cert: %v", err)
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	certPool := x509.NewCertPool()
	certPool.AppendCertsFromPEM(caPEM)
	return &tls.Config{
		MinVersion: tls.VersionTLS13,
		RootCAs:    certPool,
	}
}

// TestV1DataEngineInstanceOps_StructureCorrect verifies that V1DataEngineInstanceOps
// has the correct fields and does NOT have spdkServiceAddress (which is only for V2).
func TestV1DataEngineInstanceOps_StructureCorrect(t *testing.T) {
	v1Ops := V1DataEngineInstanceOps{}

	v1Type := reflect.TypeOf(v1Ops)

	// V1 should have exactly 2 fields: processManagerServiceAddress and clientTLSConfig
	expectedFieldCount := 2
	actualFieldCount := v1Type.NumField()

	if actualFieldCount != expectedFieldCount {
		t.Errorf("V1DataEngineInstanceOps should have %d fields, but has %d", expectedFieldCount, actualFieldCount)
	}

	_, hasProcessManager := v1Type.FieldByName("processManagerServiceAddress")
	if !hasProcessManager {
		t.Error("V1DataEngineInstanceOps should have processManagerServiceAddress field")
	}

	_, hasClientTLS := v1Type.FieldByName("clientTLSConfig")
	if !hasClientTLS {
		t.Error("V1DataEngineInstanceOps should have clientTLSConfig field")
	}

	_, hasSPDK := v1Type.FieldByName("spdkServiceAddress")
	if hasSPDK {
		t.Error("V1DataEngineInstanceOps should NOT have spdkServiceAddress field (V1 data engine only uses ProcessManager)")
	}
}

// TestV1DataEngineInstanceOps_TLSConfigPropagation verifies that V1DataEngineInstanceOps
// receives the correct clientTLSConfig and processManagerServiceAddress when NewServer is called.
func TestV1DataEngineInstanceOps_TLSConfigPropagation(t *testing.T) {
	ctx := context.Background()
	logsDir := t.TempDir()
	processManagerServiceAddress := "localhost:8500"
	spdkServiceAddress := "localhost:8504"
	tlsConfig := buildTestTLSConfig(t)

	server, err := NewServer(ctx, logsDir, processManagerServiceAddress, spdkServiceAddress, tlsConfig, false)

	if err != nil {
		t.Fatalf("NewServer should succeed, but got error: %v", err)
	}
	if server == nil {
		t.Fatal("Server should not be nil")
	}

	v1Ops, ok := server.ops[rpc.DataEngine_DATA_ENGINE_V1].(V1DataEngineInstanceOps)
	if !ok {
		t.Fatal("ops[DATA_ENGINE_V1] should be V1DataEngineInstanceOps type")
	}

	if v1Ops.clientTLSConfig == nil {
		t.Error("V1 ops clientTLSConfig should not be nil when TLS is enabled")
	}

	if v1Ops.processManagerServiceAddress != processManagerServiceAddress {
		t.Errorf("V1 ops processManagerServiceAddress = %q, want %q", v1Ops.processManagerServiceAddress, processManagerServiceAddress)
	}
}

// TestV2DataEngineInstanceOps_TLSConfigPropagation verifies that V2DataEngineInstanceOps
// receives the correct spdkTLSConfig and spdkServiceAddress when NewServer is called.
func TestV2DataEngineInstanceOps_TLSConfigPropagation(t *testing.T) {
	ctx := context.Background()
	logsDir := t.TempDir()
	processManagerServiceAddress := "localhost:8500"
	spdkServiceAddress := "localhost:8504"
	tlsConfig := buildTestTLSConfig(t)

	server, err := NewServer(ctx, logsDir, processManagerServiceAddress, spdkServiceAddress, tlsConfig, true)

	if err != nil {
		t.Fatalf("NewServer should succeed, but got error: %v", err)
	}
	if server == nil {
		t.Fatal("Server should not be nil")
	}

	v2Ops, ok := server.ops[rpc.DataEngine_DATA_ENGINE_V2].(V2DataEngineInstanceOps)
	if !ok {
		t.Fatal("ops[DATA_ENGINE_V2] should be V2DataEngineInstanceOps type")
	}

	if v2Ops.spdkTLSConfig == nil {
		t.Error("V2 ops spdkTLSConfig should not be nil when TLS is enabled")
	}

	if v2Ops.spdkServiceAddress != spdkServiceAddress {
		t.Errorf("V2 ops spdkServiceAddress = %q, want %q", v2Ops.spdkServiceAddress, spdkServiceAddress)
	}
}

// TestNewServer_WithoutTLS verifies that Server can be created without TLS,
// ensuring backwards compatibility when no TLS config is provided.
func TestNewServer_WithoutTLS(t *testing.T) {
	ctx := context.Background()
	logsDir := t.TempDir()
	processManagerServiceAddress := "localhost:8500"
	spdkServiceAddress := "localhost:8504"

	server, err := NewServer(ctx, logsDir, processManagerServiceAddress, spdkServiceAddress, nil, false)

	if err != nil {
		t.Fatalf("NewServer should succeed without TLS, but got error: %v", err)
	}
	if server == nil {
		t.Fatal("Server should not be nil")
	}

	v1Ops, ok := server.ops[rpc.DataEngine_DATA_ENGINE_V1].(V1DataEngineInstanceOps)
	if !ok {
		t.Fatal("ops[DATA_ENGINE_V1] should be V1DataEngineInstanceOps type")
	}

	if v1Ops.clientTLSConfig != nil {
		t.Error("V1 ops clientTLSConfig should be nil when TLS is not provided")
	}

	v2Ops, ok := server.ops[rpc.DataEngine_DATA_ENGINE_V2].(V2DataEngineInstanceOps)
	if !ok {
		t.Fatal("ops[DATA_ENGINE_V2] should be V2DataEngineInstanceOps type")
	}

	if v2Ops.spdkTLSConfig != nil {
		t.Error("V2 ops spdkTLSConfig should be nil when TLS is not provided")
	}
}

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
// MUST stamp the correct InstanceType, pass through the SPDK-reported state,
// and surface the transport-specific fields (per-path transport, replica
// tcp/rdma ports) the manager publishes on the CRDs.
func TestEngineFrontendResponseToInstanceResponse(t *testing.T) {
	ef := &spdkapi.EngineFrontend{
		Name:       "pvc-test-ef-0",
		EngineName: "pvc-test-e-0",
		Endpoint:   "/dev/longhorn/pvc-test",
		Frontend:   "spdk-tcp-blockdev",
		State:      instanceStateRunning,
		Paths: []*spdkapi.EngineFrontendNvmeTCPPath{
			{TargetIP: "10.0.0.1", TargetPort: 20001, NQN: "nqn.test", ANAState: "optimized", Transport: "rdma"},
		},
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
	if len(got.Status.Paths) != 1 {
		t.Fatalf("paths: got %d, want 1", len(got.Status.Paths))
	}
	if got.Status.Paths[0].Transport != "rdma" {
		t.Errorf("path transport: got %q, want %q (manager publishes this on the EngineFrontend CRD)", got.Status.Paths[0].Transport, "rdma")
	}
	if got.Status.Paths[0].AnaState != "optimized" {
		t.Errorf("path anaState: got %q, want %q", got.Status.Paths[0].AnaState, "optimized")
	}
}

func TestReplicaResponseToInstanceResponseTransportPorts(t *testing.T) {
	got := replicaResponseToInstanceResponse(&spdkapi.Replica{
		Name:      "pvc-test-r-0",
		State:     instanceStateRunning,
		PortStart: 20001,
		PortEnd:   20016,
		TcpPort:   20001,
		RdmaPort:  20002,
	})
	if got.Spec.Type != types.InstanceTypeReplica {
		t.Errorf("type: got %q, want %q", got.Spec.Type, types.InstanceTypeReplica)
	}
	if got.Status.State != instanceStateRunning {
		t.Errorf("state: got %q, want %q", got.Status.State, instanceStateRunning)
	}
	if got.Status.TcpPort != 20001 {
		t.Errorf("tcpPort: got %d, want %d", got.Status.TcpPort, 20001)
	}
	if got.Status.RdmaPort != 20002 {
		t.Errorf("rdmaPort: got %d, want %d", got.Status.RdmaPort, 20002)
	}
}

// imrpcTransportMapToSPDKRPC bridges two structurally identical generated
// messages. These tests pin the empty-map passthrough (the engine must see
// nil so it falls back to ReplicaAddressMap) and the per-entry mapping.
func TestImrpcTransportMapToSPDKRPCEmpty(t *testing.T) {
	if got := imrpcTransportMapToSPDKRPC(nil); got != nil {
		t.Errorf("nil input: got %v, want nil", got)
	}
	if got := imrpcTransportMapToSPDKRPC(map[string]*rpc.ReplicaTransportAddresses{}); got != nil {
		t.Errorf("empty input: got %v, want nil (engine falls back to ReplicaAddressMap)", got)
	}
}

func TestImrpcTransportMapToSPDKRPCFields(t *testing.T) {
	in := map[string]*rpc.ReplicaTransportAddresses{
		"pvc-test-r-0": {TcpAddress: "10.0.0.1:20001", RdmaAddress: "10.1.0.1:20002"},
		"pvc-test-r-1": nil, // nil entries are skipped, not copied
	}

	got := imrpcTransportMapToSPDKRPC(in)
	if len(got) != 1 {
		t.Fatalf("entries: got %d, want 1 (nil entries must be dropped)", len(got))
	}
	addrs := got["pvc-test-r-0"]
	if addrs == nil {
		t.Fatal("pvc-test-r-0: missing entry")
	}
	if addrs.TcpAddress != "10.0.0.1:20001" {
		t.Errorf("tcpAddress: got %q, want %q", addrs.TcpAddress, "10.0.0.1:20001")
	}
	if addrs.RdmaAddress != "10.1.0.1:20002" {
		t.Errorf("rdmaAddress: got %q, want %q", addrs.RdmaAddress, "10.1.0.1:20002")
	}
}
