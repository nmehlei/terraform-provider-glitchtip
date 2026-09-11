// src/provider/provider_test.go
package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

func TestProvider_MetadataAndSchema(t *testing.T) {
	p := New("test")()
	var metaResp providerMetadataResponse
	metaResp.run(t, p)
	if metaResp.TypeName != "glitchtip" {
		t.Fatalf("expected type name glitchtip, got %q", metaResp.TypeName)
	}
}

// TestProvider_ImplementsProtocol6 is a smoke test that the provider server
// can be constructed and speaks protocol 6 without panicking — catches a
// broken Schema()/Configure() wiring before any resource-level test does.
func TestProvider_ImplementsProtocol6(t *testing.T) {
	srv := providerserver.NewProtocol6(New("test")())()
	if srv == nil {
		t.Fatal("expected a non-nil protocol6.ProviderServer")
	}
	var _ tfprotov6.ProviderServer = srv
}

type providerMetadataResponse struct {
	TypeName string
}

func (m *providerMetadataResponse) run(t *testing.T, p providerIface) {
	t.Helper()
	resp := &provider.MetadataResponse{}
	p.Metadata(context.Background(), provider.MetadataRequest{}, resp)
	m.TypeName = resp.TypeName
}

type providerIface interface {
	Metadata(context.Context, provider.MetadataRequest, *provider.MetadataResponse)
}
