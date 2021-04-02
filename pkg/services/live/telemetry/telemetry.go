package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana/pkg/api/response"
	"github.com/grafana/grafana/pkg/plugins/backendplugin/coreplugin"
	"github.com/grafana/grafana/pkg/util"

	"github.com/grafana/grafana-live-sdk/telemetry/telegraf"
	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/grafana/grafana/pkg/bus"
	"github.com/grafana/grafana/pkg/infra/localcache"
	"github.com/grafana/grafana/pkg/infra/log"
	"github.com/grafana/grafana/pkg/models"
	"github.com/grafana/grafana/pkg/plugins/manager"
	"github.com/grafana/grafana/pkg/registry"
	"github.com/grafana/grafana/pkg/services/datasources"
	"github.com/grafana/grafana/pkg/services/live"
	"github.com/grafana/grafana/pkg/setting"
)

var (
	logger = log.New("live_push")
)

func init() {
	registry.RegisterServiceWithPriority(&Receiver{}, registry.Low)
}

// Receiver proxies telemetry requests to Grafana Live system.
type Receiver struct {
	Cfg             *setting.Cfg             `inject:""`
	PluginManager   *manager.PluginManager   `inject:""`
	Bus             bus.Bus                  `inject:""`
	CacheService    *localcache.CacheService `inject:""`
	DatasourceCache datasources.CacheService `inject:""`
	GrafanaLive     *live.GrafanaLive        `inject:""`

	cache                         *Cache2
	telegrafConverterWide         *telegraf.Converter
	telegrafConverterLabelsColumn *telegraf.Converter
}

// Init Receiver.
func (t *Receiver) Init() error {
	logger.Info("Telemetry Receiver initialization")

	if !t.IsEnabled() {
		logger.Debug("Telemetry Receiver not enabled, skipping initialization")
		return nil
	}

	// For now only Telegraf converter (influx format) is supported.
	t.telegrafConverterWide = telegraf.NewConverter()
	t.telegrafConverterLabelsColumn = telegraf.NewConverter(telegraf.WithUseLabelsColumn(true))

	t.cache = NewCache2()

	factory := coreplugin.New(backend.ServeOpts{
		StreamHandler: newTelemetryStreamHandler(t.cache),
	})
	err := t.PluginManager.BackendPluginManager.Register("live-push", factory)
	if err != nil {
		return err
	}
	return nil
}

// Run Receiver.
func (t *Receiver) Run(ctx context.Context) error {
	if !t.IsEnabled() {
		logger.Debug("GrafanaLive feature not enabled, skipping initialization of Telemetry Receiver")
		return nil
	}
	<-ctx.Done()
	return ctx.Err()
}

// IsEnabled returns true if the Grafana Live feature is enabled.
func (t *Receiver) IsEnabled() bool {
	return t.Cfg.IsLiveEnabled() // turn on when Live on for now.
}

func (t *Receiver) HandleList(ctx *models.ReqContext) response.Response {

	info := util.DynMap{}

	for k, v := range t.cache.ids {
		sub := util.DynMap{}
		for sK, sV := range v.schemas {
			sub[sK] = sV
		}
		info[k] = sub
	}

	return response.JSONStreaming(200, info)
}

func (t *Receiver) Handle(ctx *models.ReqContext) {
	slug := ctx.Req.URL.Path
	slug = strings.TrimPrefix(slug, "/api/live/push/")
	// TODO should not be called "path" since it is just one slug?

	if len(slug) < 1 || strings.Contains(slug, "/") {
		logger.Error("invalid slug", "slug", slug)
		ctx.Resp.WriteHeader(http.StatusBadRequest)
		return
	}

	cache := t.cache.GetOrCreate(slug)

	converter := t.telegrafConverterWide
	if ctx.Req.URL.Query().Get("format") == "labels_column" {
		converter = t.telegrafConverterLabelsColumn
	}

	body, err := ctx.Req.Body().Bytes()
	if err != nil {
		logger.Error("Error reading body", "error", err)
		ctx.Resp.WriteHeader(http.StatusInternalServerError)
		return
	}
	logger.Debug("Telemetry request body", "body", string(body), "path", slug)

	metricFrames, err := converter.Convert(body)
	if err != nil {
		logger.Error("Error converting metrics", "error", err)
		ctx.Resp.WriteHeader(http.StatusInternalServerError)
		return
	}

	for _, mf := range metricFrames {
		frame := mf.Frame()
		frameSchema, err := data.FrameToJSON(frame, true, false)
		if err != nil {
			logger.Error("Error marshaling Frame to Schema", "error", err)
			ctx.Resp.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, ok, _ := cache.Get(mf.Key())
		_ = cache.Update(mf.Key(), frameSchema)
		frameData, err := data.FrameToJSON(mf.Frame(), !ok, true)
		if err != nil {
			logger.Error("Error marshaling Frame to JSON", "error", err)
			ctx.Resp.WriteHeader(http.StatusInternalServerError)
			return
		}
		// TODO: need a proper path validation (but for now pass it as part of channel name).
		channel := fmt.Sprintf("push/%s/%s", slug, mf.Key())
		logger.Debug("publish data to channel", "channel", channel, "data", string(frameData))
		err = t.GrafanaLive.Publish(channel, frameData)
		if err != nil {
			logger.Error("Error publishing to a channel", "error", err, "channel", channel)
			ctx.Resp.WriteHeader(http.StatusInternalServerError)
			return
		}
	}
}
