package dlna

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestSearchTargetsAreCaseInsensitiveAndComplete(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	r := &Receiver{}

	direct := r.searchTargets("URN:SCHEMAS-UPNP-ORG:DEVICE:MEDIARENDERER:1")
	if len(direct) != 1 || direct[0] != rendererType {
		t.Fatalf("MediaRenderer search returned %q", direct)
	}

	all := r.searchTargets("ssdp:all")
	if len(all) != 6 {
		t.Fatalf("ssdp:all returned %d targets, want 6: %q", len(all), all)
	}
	for _, wanted := range []string{rendererType, avTransportType, connectionManagerType, renderingControlType} {
		if !contains(all, wanted) {
			t.Errorf("ssdp:all is missing %q", wanted)
		}
	}
}

func TestDeviceDescriptionAdvertisesDLNARendererServices(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	xml := (&Receiver{}).deviceXML()
	for _, wanted := range []string{"<friendlyName>" + FriendlyName() + "</friendlyName>", "DMR-1.50", avTransportType, connectionManagerType, renderingControlType} {
		if !strings.Contains(xml, wanted) {
			t.Errorf("device description is missing %q", wanted)
		}
	}
	assertWellFormedXML(t, xml)
	for _, scpd := range []string{avTransportSCPD, connectionManagerSCPD, renderingControlSCPD} {
		assertWellFormedXML(t, scpd)
	}
}

func assertWellFormedXML(t *testing.T, value string) {
	t.Helper()
	var document struct{}
	if err := xml.Unmarshal([]byte(value), &document); err != nil {
		t.Fatalf("invalid XML: %v", err)
	}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
