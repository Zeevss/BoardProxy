package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/Zeevss/BoardProxy/subscribe/internal/config"
	"github.com/Zeevss/BoardProxy/subscribe/internal/web"
	"github.com/Zeevss/BoardProxy/subscribe/protocol"
)

type previewResolver struct{}

func (previewResolver) ResolveToken(context.Context, string) (protocol.Subscription, error) {
	return protocol.Subscription{
		Version: 1, ID: "preview", Name: "Алексей", State: "enabled", Revision: "preview-1",
		IssuedAt: time.Now().UTC(), UsedBytes: 14_216_347_648, TrafficLimit: 107_374_182_400,
		Keys: []protocol.Key{
			{ID: "de-fra", Name: "Германия · Frankfurt", NodeID: "de-fra-01", UserID: "alex", State: "enabled", UsedBytes: 8_750_923_776, Keylink: "bproxy://preview-one"},
			{ID: "nl-ams", Name: "Нидерланды · Amsterdam", NodeID: "nl-ams-02", UserID: "alex", State: "enabled", UsedBytes: 5_465_423_872, Keylink: "bproxy://preview-two"},
			{ID: "fi-hel", Name: "Финляндия · Helsinki", NodeID: "fi-hel-01", UserID: "alex", State: "disabled", UsedBytes: 0},
		},
	}, nil
}

func main() {
	listen := flag.String("listen", "127.0.0.1:8091", "preview listen address")
	flag.Parse()
	handler := web.New(previewResolver{}, []config.App{
		{Name: "BoardProxy для Android", URL: "https://example.com/android"},
	}, func() bool { return true }).Routes()
	log.Printf("subscription preview: http://%s/s/preview", *listen)
	log.Fatal(http.ListenAndServe(*listen, handler))
}
