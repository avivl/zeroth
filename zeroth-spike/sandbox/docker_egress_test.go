package sandbox_test

import (
	"context"
	"strings"
	"testing"

	"github.com/avivl/zeroth/zeroth-spike/sandbox"
	"github.com/avivl/zeroth/zeroth-spike/session"
)

func TestDockerNetworkNoneDeniesEgress(t *testing.T) {
	d := requireDocker(t)
	tarPath := writeHelloTar(t)
	id, err := session.ParseID("sess-docker-deny")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	inst, err := d.Start(ctx, sandbox.StartRequest{
		SessionID: id,
		Workspace: sandbox.Workspace{TarPath: tarPath},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = inst.Stop(ctx) })

	res, err := inst.Exec(ctx, []string{"sh", "-c", "echo $HTTP_PROXY"})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if strings.TrimSpace(res.Stdout) != "" {
		t.Fatalf("HTTP_PROXY set with empty allowlist: %q", res.Stdout)
	}

	res, err = inst.Exec(ctx, []string{"ls", "/sys/class/net"})
	if err != nil {
		t.Fatalf("exec nets: %v", err)
	}
	ifaces := strings.Fields(res.Stdout)
	for _, iface := range ifaces {
		if iface != "lo" {
			t.Fatalf("network none has extra iface %q (%q)", iface, res.Stdout)
		}
	}
}

func TestDockerLeasedEgressSetsProxy(t *testing.T) {
	d := requireDocker(t)
	tarPath := writeHelloTar(t)
	id, err := session.ParseID("sess-docker-allow")
	if err != nil {
		t.Fatal(err)
	}
	allow, err := sandbox.AllowlistFromLeases([]sandbox.EgressLease{
		{Destinations: []string{"example.com:443"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	inst, err := d.Start(ctx, sandbox.StartRequest{
		SessionID: id,
		Workspace: sandbox.Workspace{TarPath: tarPath},
		Egress:    allow,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = inst.Stop(ctx) })

	res, err := inst.Exec(ctx, []string{"sh", "-c", "echo $HTTP_PROXY"})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	got := strings.TrimSpace(res.Stdout)
	if !strings.Contains(got, "host.docker.internal:") {
		t.Fatalf("HTTP_PROXY = %q", got)
	}
}
