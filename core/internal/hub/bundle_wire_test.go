package hub

import (
	"testing"

	"bproxy-core/internal/bond"
)

func TestBundleRequestRoundTrip(t *testing.T) {
	id, err := bond.NewBundleID()
	if err != nil {
		t.Fatal(err)
	}
	req, ok := decodeBundleRequest(encodeNewBundleRequest(id))
	if !ok || req.kind != bundleRequestNew || req.id != id {
		t.Fatalf("new request = %+v ok=%v", req, ok)
	}

	token, err := bond.NewJoinToken()
	if err != nil {
		t.Fatal(err)
	}
	req, ok = decodeBundleRequest(encodeJoinBundleRequest(id, 7, token))
	if !ok || req.kind != bundleRequestJoin || req.id != id || req.epoch != 7 || !req.token.Equal(token) {
		t.Fatalf("join request = %+v ok=%v", req, ok)
	}
}

func TestBundleAssignmentRoundTrip(t *testing.T) {
	id, _ := bond.NewBundleID()
	token, _ := bond.NewJoinToken()
	want := bundleAssignment{id: id, lane: 3, epoch: 2, token: token, page: "page-hash"}
	raw, ok := encodeBundleAssignment(want)
	if !ok {
		t.Fatal("encodeBundleAssignment rejected valid assignment")
	}
	got, ok := decodeBundleAssignment(raw)
	if !ok || got.id != want.id || got.lane != want.lane || got.epoch != want.epoch ||
		!got.token.Equal(want.token) || got.page != want.page {
		t.Fatalf("assignment = %+v ok=%v, want %+v", got, ok, want)
	}
}

func TestBundleWireRejectsMalformedValues(t *testing.T) {
	if _, ok := decodeBundleRequest([]byte{byte(bundleRequestNew)}); ok {
		t.Fatal("short bundle request accepted")
	}
	if _, ok := encodeBundleAssignment(bundleAssignment{}); ok {
		t.Fatal("empty bundle assignment accepted")
	}
	if _, ok := decodeBundleAssignment([]byte("short")); ok {
		t.Fatal("short bundle assignment accepted")
	}
}
