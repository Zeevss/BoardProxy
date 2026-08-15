package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strings"

	"github.com/Zeevss/BoardProxy/subscribe/internal/config"
	"github.com/Zeevss/BoardProxy/subscribe/internal/controlplane"
	"github.com/Zeevss/BoardProxy/subscribe/protocol"
	"github.com/skip2/go-qrcode"
)

type Resolver interface {
	ResolveToken(ctx context.Context, token string) (protocol.Subscription, error)
}

type Handler struct {
	resolver      Resolver
	apps          []config.App
	page          *template.Template
	recoveryReady func() bool
}

func New(resolver Resolver, apps []config.App, ready func() bool) *Handler {
	page := template.Must(template.New("page").Funcs(template.FuncMap{"bytes": formatBytes}).Parse(pageHTML))
	return &Handler{resolver: resolver, apps: apps, recoveryReady: ready, page: page}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /favicon.ico", func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("GET /healthz", func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /readyz", h.readiness)
	mux.HandleFunc("GET /recoveryz", h.recoveryReadiness)
	mux.HandleFunc("GET /s/{token}", h.subscription)
	mux.HandleFunc("POST /s/{token}/qr", h.qr)
	return securityHeaders(mux)
}

func (h *Handler) readiness(writer http.ResponseWriter, _ *http.Request) {
	writer.WriteHeader(http.StatusOK)
}

func (h *Handler) recoveryReadiness(writer http.ResponseWriter, _ *http.Request) {
	if h.recoveryReady != nil && !h.recoveryReady() {
		http.Error(writer, "Yandex recovery watcher is not ready", http.StatusServiceUnavailable)
		return
	}
	writer.WriteHeader(http.StatusOK)
}

func (h *Handler) subscription(writer http.ResponseWriter, request *http.Request) {
	snapshot, err := h.resolver.ResolveToken(request.Context(), request.PathValue("token"))
	if err != nil {
		writeResolveError(writer, err)
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	if strings.Contains(request.Header.Get("Accept"), protocol.MediaType) {
		writer.Header().Set("Content-Type", protocol.MediaType)
		_ = json.NewEncoder(writer).Encode(snapshot)
		return
	}
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.page.Execute(writer, map[string]any{"Subscription": snapshot, "Apps": h.apps}); err != nil {
		return
	}
}

func (h *Handler) qr(writer http.ResponseWriter, request *http.Request) {
	if _, err := h.resolver.ResolveToken(request.Context(), request.PathValue("token")); err != nil {
		writeResolveError(writer, err)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(request.Body, 4<<10))
	if err != nil || len(raw) == 0 {
		http.Error(writer, "invalid QR payload", http.StatusBadRequest)
		return
	}
	_, token, _, err := protocol.ParseURL(string(raw))
	if err != nil || token != request.PathValue("token") {
		http.Error(writer, "QR payload does not match the subscription", http.StatusBadRequest)
		return
	}
	png, err := qrcode.Encode(string(raw), qrcode.Medium, 256)
	if err != nil {
		http.Error(writer, "cannot encode QR", http.StatusBadRequest)
		return
	}
	writer.Header().Set("Content-Type", "image/png")
	writer.Header().Set("Cache-Control", "no-store")
	_, _ = writer.Write(png)
}

func writeResolveError(writer http.ResponseWriter, err error) {
	var status *controlplane.StatusError
	if errors.As(err, &status) {
		switch status.Status {
		case http.StatusNotFound:
			http.Error(writer, "subscription not found", http.StatusNotFound)
		case http.StatusForbidden, http.StatusGone:
			http.Error(writer, "subscription is unavailable", status.Status)
		default:
			http.Error(writer, "subscription backend unavailable", http.StatusBadGateway)
		}
		return
	}
	http.Error(writer, "subscription backend unavailable", http.StatusBadGateway)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasPrefix(request.URL.Path, "/s/") {
			writer.Header().Set("Cache-Control", "no-store")
		}
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("X-Frame-Options", "DENY")
		writer.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'; img-src 'self' blob:; base-uri 'none'; frame-ancestors 'none'")
		next.ServeHTTP(writer, request)
	})
}

const pageHTML = `<!doctype html>
<html lang="ru"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover">
<meta name="color-scheme" content="light"><meta name="theme-color" content="#f3f7f5">
<title>{{.Subscription.Name}} — BoardProxy</title><style>
:root{--ink:#10231c;--muted:#65736d;--line:#dfe8e3;--surface:#fff;--soft:#f3f7f5;--accent:#176b4b;--accent-dark:#0f5138;--danger:#9b463c;--shadow:0 24px 70px rgba(20,54,42,.10)}*{box-sizing:border-box}body{margin:0;min-width:320px;background:radial-gradient(circle at 15% 0,#e2f2e9 0,transparent 34rem),var(--soft);color:var(--ink);font:15px/1.5 Inter,ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;-webkit-font-smoothing:antialiased}.shell{width:min(880px,100%);margin:0 auto;padding:28px 20px 48px}.brand{display:flex;align-items:center;gap:11px;margin-bottom:24px;color:var(--ink);font-weight:750;letter-spacing:-.02em}.mark{display:grid;place-items:center;width:36px;height:36px;border-radius:11px;background:var(--ink);color:#fff;font-size:13px;letter-spacing:-.03em}.hero{position:relative;overflow:hidden;padding:34px;border:1px solid rgba(255,255,255,.75);border-radius:28px;background:rgba(255,255,255,.92);box-shadow:var(--shadow)}.hero:after{content:"";position:absolute;right:-90px;top:-120px;width:270px;height:270px;border-radius:50%;background:#d8eee3;opacity:.72}.hero-copy{position:relative;z-index:1;max-width:620px}.status{display:inline-flex;align-items:center;gap:8px;color:var(--accent-dark);font-size:13px;font-weight:700}.status:before{content:"";width:8px;height:8px;border-radius:50%;background:#2ca46f;box-shadow:0 0 0 5px #2ca46f18}h1{margin:12px 0 8px;font-size:clamp(32px,6vw,52px);line-height:1.04;letter-spacing:-.045em}.lead{margin:0;color:var(--muted);font-size:16px}.stats{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:1px;margin-top:28px;overflow:hidden;border:1px solid var(--line);border-radius:17px;background:var(--line)}.stat{min-width:0;padding:17px 18px;background:var(--surface)}.stat span{display:block;color:var(--muted);font-size:12px}.stat strong{display:block;margin-top:3px;overflow:hidden;text-overflow:ellipsis;font-size:21px;letter-spacing:-.03em;white-space:nowrap}.content{display:grid;grid-template-columns:minmax(0,1.35fr) minmax(260px,.65fr);gap:18px;margin-top:18px}.panel{padding:24px;border:1px solid var(--line);border-radius:22px;background:var(--surface)}.panel h2{margin:0 0 4px;font-size:19px;letter-spacing:-.025em}.panel-intro{margin:0 0 17px;color:var(--muted);font-size:13px}.keys{display:grid;gap:10px}.key{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:8px 14px;padding:15px;border:1px solid var(--line);border-radius:15px;background:#fbfdfc}.key strong{overflow-wrap:anywhere}.key small{grid-column:1;color:var(--muted)}.key-traffic{color:var(--muted);font-size:12px}.state{align-self:start;font-size:12px;font-weight:700}.ok{color:var(--accent)}.off{color:var(--danger)}.access{display:flex;flex-direction:column}.access button,.app{width:100%;min-height:46px;border:0;border-radius:13px;font:700 14px/1.2 inherit;text-align:center;text-decoration:none;cursor:pointer}.access button{padding:13px 16px;background:var(--accent);color:#fff}.access button:hover{background:var(--accent-dark)}.access button:focus-visible,.app:focus-visible{outline:3px solid #62ae8d;outline-offset:3px}#qr{display:none;width:min(100%,256px);height:auto;margin:18px auto 4px;border:10px solid #fff;border-radius:12px;box-shadow:0 12px 30px #183b2d18}.qr-note{margin:11px 0 0;color:var(--muted);font-size:12px;text-align:center}.apps{display:grid;gap:9px;margin-top:18px;padding-top:18px;border-top:1px solid var(--line)}.app{display:grid;place-items:center;padding:12px 14px;background:var(--ink);color:#fff}.app:hover{background:#1b382d}.empty{padding:18px;color:var(--muted);text-align:center;border:1px dashed var(--line);border-radius:15px}@media(max-width:700px){.shell{padding:18px 14px 34px}.brand{margin:2px 4px 18px}.hero{padding:25px 20px;border-radius:22px}.hero:after{right:-150px;top:-150px}.lead{font-size:15px}.stats{grid-template-columns:1fr 1fr}.stat:last-child{grid-column:1/-1}.content{grid-template-columns:1fr}.panel{padding:20px;border-radius:19px}.access{order:-1}.key{grid-template-columns:minmax(0,1fr) auto}}@media(max-width:390px){h1{font-size:31px}.stats{grid-template-columns:1fr}.stat:last-child{grid-column:auto}.key{grid-template-columns:1fr}.state{grid-row:1;grid-column:2}.key small{grid-column:1/-1}.panel{padding:17px}.hero{padding:22px 17px}}@media(prefers-reduced-motion:no-preference){.hero,.panel{animation:enter .42s ease-out both}.panel{animation-delay:.06s}@keyframes enter{from{opacity:0;transform:translateY(8px)}to{opacity:1;transform:none}}}
</style></head><body><main class="shell"><header class="brand"><span class="mark" aria-hidden="true">BP</span><span>BoardProxy</span></header>
<section class="hero"><div class="hero-copy"><div class="status">Подписка активна</div><h1>{{.Subscription.Name}}</h1><p class="lead">Одна ссылка автоматически обновляет все доступные подключения.</p>
<div class="stats"><div class="stat"><span>Использовано</span><strong>{{bytes .Subscription.UsedBytes}}</strong></div><div class="stat"><span>Лимит</span><strong>{{if .Subscription.TrafficLimit}}{{bytes .Subscription.TrafficLimit}}{{else}}Без лимита{{end}}</strong></div><div class="stat"><span>Подключений</span><strong>{{len .Subscription.Keys}}</strong></div></div></div></section>
<div class="content"><section class="panel"><h2>Ваши подключения</h2><p class="panel-intro">Клиент выберет доступное подключение автоматически.</p><div class="keys">{{range .Subscription.Keys}}<article class="key"><strong>{{.Name}}</strong><span class="state {{if eq .State "enabled"}}ok{{else}}off{{end}}">{{if eq .State "enabled"}}Доступно{{else}}Недоступно{{end}}</span><small>{{.NodeID}} · <span class="key-traffic">{{bytes .UsedBytes}}</span></small></article>{{else}}<div class="empty">Доступных подключений пока нет.</div>{{end}}</div></section>
<aside class="panel access"><h2>Подключить устройство</h2><p class="panel-intro">Отсканируйте QR-код в приложении BoardProxy.</p><button id="show" type="button">Показать QR-код</button><img id="qr" alt="QR-код подписки" width="256" height="256"><p id="qr-note" class="qr-note">Ссылка содержит секрет доступа — не публикуйте её.</p>{{if .Apps}}<div class="apps">{{range .Apps}}<a class="app" href="{{.URL}}">Скачать {{.Name}}</a>{{end}}</div>{{end}}</aside></div></main><script>
const button=document.getElementById('show'),image=document.getElementById('qr'),note=document.getElementById('qr-note');button.onclick=async()=>{button.disabled=true;button.textContent='Создаём QR…';try{const response=await fetch(location.pathname+'/qr',{method:'POST',headers:{'Content-Type':'text/plain'},body:location.href});if(!response.ok)throw new Error();image.src=URL.createObjectURL(await response.blob());image.style.display='block';button.textContent='QR-код готов';note.textContent='Откройте приложение и отсканируйте код.'}catch{button.disabled=false;button.textContent='Попробовать ещё раз';note.textContent='Не удалось создать QR-код.'}};
</script></body></html>`

func formatBytes(value uint64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d Б", value)
	}
	divisor, exponent := uint64(unit), 0
	for amount := value / unit; amount >= unit && exponent < 4; amount /= unit {
		divisor *= unit
		exponent++
	}
	units := []rune("КМГТП")
	return fmt.Sprintf("%.1f %cБ", float64(value)/float64(divisor), units[exponent])
}
