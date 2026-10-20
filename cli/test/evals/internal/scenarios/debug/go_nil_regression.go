package debug

import (
	"fmt"
	"time"

	"github.com/tracewayapp/traceway/cli/test/evals/internal/scenarios"
	"github.com/tracewayapp/traceway/cli/test/evals/internal/seed"
)

func init() {
	scenarios.Register("debug/go-nil-regression", seedGoNilRegression)
}

// goPanic is the stack a recover middleware records for a panic, with
// debug.Stack()'s frames above the panicking one.
func goPanic(goroutine int, frames ...string) string {
	s := fmt.Sprintf("goroutine %d [running]:\nruntime/debug.Stack()\n\t/usr/local/go/src/runtime/debug/stack.go:26 +0x5e\n", goroutine)
	s += "example.com/billing-api/internal/invoices.Recover.func1.1()\n\t/app/internal/invoices/handler.go:45 +0x9c\n"
	s += "panic({0x1a2b3c0?, 0x2a4f8b0?})\n\t/usr/local/go/src/runtime/panic.go:791 +0x132\n"
	for _, f := range frames {
		s += f + "\n"
	}
	return s
}

// billingService is the invoice service shared by the billing debug cases:
// v1.8.2, deployed at deployAt, renders the billing address without a nil
// check, so customers who never entered one get a panic.
func billingService(s *seed.Scenario, deployAt time.Time) (*seed.Project, *seed.Service, *seed.Endpoint) {
	p := s.Project("billing", seed.OTel)
	api := p.Service("billing-api", seed.Go("nethttp", "sql")).Versions("1.8.1", seed.Deploy{At: deployAt, Version: "1.8.2"})
	nilStack := goPanic(1183,
		"example.com/billing-api/internal/invoices.formatAddress(...)\n\t/app/internal/invoices/render.go:37",
		"example.com/billing-api/internal/invoices.render(0xc00041a2d0)\n\t/app/internal/invoices/render.go:27 +0x14d",
		"example.com/billing-api/internal/invoices.(*Handler).Get(0xc000186a50, {0x1d1e2a8, 0xc0004d6000}, 0xc0004c8500)\n\t/app/internal/invoices/handler.go:34 +0x2f4",
		"net/http.HandlerFunc.ServeHTTP(0xc000189480, {0x1d1e2a8, 0xc0004d6000}, 0xc0004c8500)\n\t/usr/local/go/src/net/http/server.go:2294 +0x29",
		"example.com/billing-api/internal/invoices.Recover.func1({0x1d1e2a8, 0xc0004d6000}, 0xc0004c8500)\n\t/app/internal/invoices/handler.go:49 +0x8a",
	)
	get := api.Endpoint("GET", "/api/invoices/:id")
	get.Traffic(seed.Daily(700), seed.Diurnal(), seed.Over(7*24*time.Hour))
	get.Detail(seed.DetailPolicy{Recent: 6 * time.Hour, Rate: 0.03, Max: 10})
	get.Handler(func(r *seed.Req) {
		customer := 4000 + r.IntN(9000)
		r.RootAttr("enduser.id", fmt.Sprint(customer))
		r.Gap(r.Ms(0.3, 0.9))
		r.DB("SELECT i.id, i.number, i.issued_at, i.total_cents, i.currency, c.id, c.name, c.email FROM invoices i JOIN customers c ON c.id = i.customer_id WHERE i.id = $1", r.Ms(2, 5))
		noAddress := r.Chance(0.14)
		r.DB("SELECT line1, line2, city, postal_code, country FROM billing_addresses WHERE customer_id = $1", r.Ms(1, 3), seed.Rows(map[bool]int{true: 0, false: 1}[noAddress]))
		if r.Version() == "1.8.2" && noAddress {
			r.Error(seed.Exc{Type: "runtime.errorString", Message: "runtime error: invalid memory address or nil pointer dereference", Stack: nilStack, Escaped: true})
			r.Log(seed.SevError, "panic serving request", seed.A("path", r.Request().Root.Attrs["url.path"]), seed.A("panic", "runtime error: invalid memory address or nil pointer dereference"))
			r.Status(500)
			r.Gap(r.Ms(0.2, 0.5))
			return
		}
		r.Compute("render invoice", r.Ms(0.4, 1.2))
		r.Gap(r.Ms(0.2, 0.5))
	})

	// Distractor: an older, unrelated issue on another route.
	send := api.Endpoint("POST", "/api/invoices/:id/send")
	send.Traffic(seed.Daily(120), seed.Diurnal(), seed.Over(7*24*time.Hour))
	send.Handler(func(r *seed.Req) {
		r.Gap(r.Ms(0.3, 0.9))
		r.DB("SELECT id, number, customer_id FROM invoices WHERE id = $1", r.Ms(2, 5))
		if r.Chance(0.02) {
			r.HTTP("POST", "https://api.mailer.example/v3/messages", 10*time.Second, 504)
			r.Error(seed.Exc{Type: "*url.Error", Message: `Post "https://api.mailer.example/v3/messages": context deadline exceeded`,
				Stack: "goroutine 902 [running]:\nexample.com/billing-api/internal/mail.(*Client).Send(0xc0002b4000, {0x1d1e2a8, 0xc0003a2000}, 0xc0004e8000)\n\t/app/internal/mail/client.go:71 +0x2e5\nexample.com/billing-api/internal/invoices.(*Handler).Send(0xc000186a50, {0x1d1e2a8, 0xc0004d6000}, 0xc0004c8500)\n\t/app/internal/invoices/send.go:40 +0x1b4"})
			r.Status(502)
			return
		}
		r.HTTP("POST", "https://api.mailer.example/v3/messages", r.LogNormal(140*time.Millisecond, 500*time.Millisecond), 202)
	})
	s.Background(api, seed.TypicalTraffic(6))
	return p, api, get
}

// seedGoNilRegression: a nil-pointer panic in invoice rendering that starts
// with the v1.8.2 deploy 30 hours ago.
func seedGoNilRegression(s *seed.Scenario) error {
	deployAt := s.Ago(30 * time.Hour).Truncate(time.Minute).Add(-7 * time.Minute)
	p, _, get := billingService(s, deployAt)
	s.Expose("deployAt", deployAt.Format(time.RFC3339))
	s.Expose("lastFailure", get.Pick(func(r *seed.Request) bool { return r.Status == 500 }))
	s.AfterFlush(func() error {
		hash, err := p.GroupMatching(8*24*time.Hour, "formatAddress")
		if err != nil {
			return err
		}
		s.Expose("hash", hash)
		other, err := p.GroupMatching(8*24*time.Hour, "mailer.example")
		if err != nil {
			return err
		}
		s.Expose("otherHash", other)
		return nil
	})
	return nil
}
