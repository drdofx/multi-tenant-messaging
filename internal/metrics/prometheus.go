package metrics

import (
	"github.com/gofiber/fiber/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/valyala/fasthttp/fasthttpadaptor"
)

// Collector holds all Prometheus metrics
type Collector struct {
	TenantConsumers    *prometheus.GaugeVec
	TenantWorkers      *prometheus.GaugeVec
	MessagesPublished  prometheus.Counter
	MessagesProcessed  *prometheus.CounterVec
	MessagesDuration   prometheus.Histogram
	QueueDepth         *prometheus.GaugeVec
	DeadLetterMessages prometheus.Counter
}

// NewCollector creates a new metrics collector
func NewCollector() *Collector {
	c := &Collector{
		TenantConsumers: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tenant_consumer_total",
				Help: "Total active consumers per tenant",
			},
			[]string{"tenant_id"},
		),
		TenantWorkers: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tenant_workers_total",
				Help: "Total workers per tenant",
			},
			[]string{"tenant_id"},
		),
		MessagesPublished: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "messages_published_total",
				Help: "Total messages published",
			},
		),
		MessagesProcessed: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "messages_processed_total",
				Help: "Total messages processed",
			},
			[]string{"status", "tenant_id"},
		),
		MessagesDuration: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "messages_processing_duration_seconds",
				Help:    "Message processing duration",
				Buckets: prometheus.DefBuckets,
			},
		),
		QueueDepth: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "queue_depth",
				Help: "Current queue depth per tenant",
			},
			[]string{"tenant_id"},
		),
		DeadLetterMessages: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "dead_letter_messages_total",
				Help: "Total messages moved to dead letter queue",
			},
		),
	}

	prometheus.MustRegister(
		c.TenantConsumers,
		c.TenantWorkers,
		c.MessagesPublished,
		c.MessagesProcessed,
		c.MessagesDuration,
		c.QueueDepth,
		c.DeadLetterMessages,
	)

	return c
}

func (c *Collector) SetTenantConsumers(tenantID string, count int) {
	c.TenantConsumers.WithLabelValues(tenantID).Set(float64(count))
}

func (c *Collector) SetTenantWorkers(tenantID string, count int) {
	c.TenantWorkers.WithLabelValues(tenantID).Set(float64(count))
}

func (c *Collector) IncMessagesPublished() {
	c.MessagesPublished.Inc()
}

func (c *Collector) IncMessagesProcessed(status, tenantID string) {
	c.MessagesProcessed.WithLabelValues(status, tenantID).Inc()
}

func (c *Collector) ObserveMessageDuration(seconds float64) {
	c.MessagesDuration.Observe(seconds)
}

func (c *Collector) SetQueueDepth(tenantID string, depth int) {
	c.QueueDepth.WithLabelValues(tenantID).Set(float64(depth))
}

func (c *Collector) IncDeadLetterMessages() {
	c.DeadLetterMessages.Inc()
}

func Handler() fiber.Handler {
	return func(c *fiber.Ctx) error {
		fasthttpadaptor.NewFastHTTPHandler(promhttp.Handler())(c.Context())
		return nil
	}
}
