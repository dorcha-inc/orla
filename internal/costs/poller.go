package costs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/harvard-cns/orla/internal/backends"
	"github.com/harvard-cns/orla/internal/settings"
)

// maxResponseBytes bounds a cost source response body. A price
// document is a few dozen bytes, anything near this limit is a
// misconfigured URL.
const maxResponseBytes = 1 << 16

// Lister is the subset of the backend registry the poller needs.
type Lister interface {
	List(ctx context.Context) ([]*backends.Backend, error)
}

// fetchTimeout bounds a single HTTP fetch. The round's own context
// bounds the whole pass at the polling interval, so a short interval
// cuts fetches off sooner than this.
const fetchTimeout = 10 * time.Second

// CostPolicy supplies the polling cadence. settings.PostgresCostStore
// satisfies it.
type CostPolicy interface {
	Get(ctx context.Context) (settings.CostConfig, error)
}

// PollerMetrics counts fetch failures so an operator can alert on a
// cost source that has stopped answering.
type PollerMetrics interface {
	IncCostFetchFailure(backend string)
}

// PollerConfig configures a Poller. Registry, Store, and Policy are
// required. Metrics is optional.
type PollerConfig struct {
	Registry Lister
	Store    *Store
	Policy   CostPolicy
	Metrics  PollerMetrics
	Logger   *slog.Logger
}

// Poller periodically fetches every configured cost source and
// updates the Store. Construct with NewPoller, start with Start, and
// join the goroutine with Close. The polling cadence is read from the
// cost policy before every round, so a control-plane change takes
// effect without a restart.
type Poller struct {
	registry Lister
	store    *Store
	policy   CostPolicy
	metrics  PollerMetrics
	logger   *slog.Logger
	client   *http.Client
	interval time.Duration

	startOnce sync.Once
	stopOnce  sync.Once
	stop      chan struct{}
	done      chan struct{}
}

// NewPoller constructs a Poller from cfg.
func NewPoller(cfg PollerConfig) *Poller {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Poller{
		registry: cfg.Registry,
		store:    cfg.Store,
		policy:   cfg.Policy,
		metrics:  cfg.Metrics,
		logger:   logger,
		client:   &http.Client{Timeout: fetchTimeout},
		interval: settings.DefaultCostRefreshInterval,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// Start launches the polling goroutine. The first round runs
// immediately so live prices are held before the first completion.
// Start is idempotent.
func (p *Poller) Start() {
	p.startOnce.Do(func() {
		go p.run()
	})
}

// Close stops the poller and waits for the goroutine to exit or ctx
// to expire. Close is idempotent.
func (p *Poller) Close(ctx context.Context) error {
	p.stopOnce.Do(func() { close(p.stop) })
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("costs: poller close: %w", ctx.Err())
	}
}

func (p *Poller) run() {
	defer close(p.done)
	ticker := time.NewTicker(p.refreshInterval())
	defer ticker.Stop()
	p.poll()
	for {
		select {
		case <-p.stop:
			return
		case <-ticker.C:
			ticker.Reset(p.refreshInterval())
			p.poll()
		}
	}
}

// refreshInterval reads the cadence from the cost policy. A read
// failure keeps the interval the poller is already running at.
func (p *Poller) refreshInterval() time.Duration {
	cfg, err := p.policy.Get(context.Background())
	if err != nil {
		p.logger.Warn("costs: read cost policy, keeping current interval",
			"interval", p.interval,
			"error", err,
		)
		return p.interval
	}
	if next := cfg.Interval(); next != p.interval {
		p.logger.Info("costs: refresh interval changed", "from", p.interval, "to", next)
		p.interval = next
	}
	return p.interval
}

// poll runs one round. A backend whose fetch fails keeps its previous
// store entry, and a backend whose source is gone leaves the store.
func (p *Poller) poll() {
	ctx, cancel := context.WithTimeout(context.Background(), p.interval)
	defer cancel()

	bs, err := p.registry.List(ctx)
	if err != nil {
		p.logger.Warn("costs: list backends", "error", err)
		return
	}

	sourced := make(map[string]bool)
	for _, b := range bs {
		if b.Kind != backends.KindLLM || b.CostSource == nil || *b.CostSource == "" {
			continue
		}
		sourced[b.Name] = true
		price, err := p.fetch(ctx, *b.CostSource)
		if err != nil {
			if p.metrics != nil {
				p.metrics.IncCostFetchFailure(b.Name)
			}
			p.logger.Warn("costs: fetch failed, keeping last known price",
				"backend", b.Name,
				"cost_source", *b.CostSource,
				"error", err,
			)
			continue
		}
		if prev, ok := p.store.Get(b.Name); !ok || prev != price {
			p.logger.Info("costs: live price updated",
				"backend", b.Name,
				"input_cost_per_mtoken", price.InputPerMtoken,
				"output_cost_per_mtoken", price.OutputPerMtoken,
			)
		}
		p.store.Set(b.Name, price)
	}
	p.store.Retain(sourced)
}

func (p *Poller) fetch(ctx context.Context, url string) (Price, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Price{}, fmt.Errorf("build request: %w", err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return Price{}, fmt.Errorf("get: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Price{}, fmt.Errorf("status %d", resp.StatusCode)
	}

	var body struct {
		InputCostPerMtoken  *float64 `json:"input_cost_per_mtoken"`
		OutputCostPerMtoken *float64 `json:"output_cost_per_mtoken"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&body); err != nil {
		return Price{}, fmt.Errorf("decode body: %w", err)
	}
	if body.InputCostPerMtoken == nil || body.OutputCostPerMtoken == nil {
		return Price{}, fmt.Errorf("input_cost_per_mtoken and output_cost_per_mtoken are both required")
	}
	if !isFiniteNonNegative(*body.InputCostPerMtoken) || !isFiniteNonNegative(*body.OutputCostPerMtoken) {
		return Price{}, fmt.Errorf("costs must be finite and non-negative, got input=%v output=%v",
			*body.InputCostPerMtoken, *body.OutputCostPerMtoken)
	}
	return Price{
		InputPerMtoken:  *body.InputCostPerMtoken,
		OutputPerMtoken: *body.OutputCostPerMtoken,
	}, nil
}

func isFiniteNonNegative(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0
}
