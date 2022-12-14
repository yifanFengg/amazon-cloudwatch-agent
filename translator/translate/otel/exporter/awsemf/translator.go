// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT

package awsemf

import (
	_ "embed"
	"fmt"

	"github.com/open-telemetry/opentelemetry-collector-contrib/exporter/awsemfexporter"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/confmap"
	"gopkg.in/yaml.v3"

	prometheus "github.com/aws/private-amazon-cloudwatch-agent-staging/translator/translate/logs/metrics_collected/prometheus"
	"github.com/aws/private-amazon-cloudwatch-agent-staging/translator/translate/logs/metrics_collected/prometheus/emfprocessor"
	"github.com/aws/private-amazon-cloudwatch-agent-staging/translator/translate/otel/common"
)

//go:embed awsemf_default_ecs.yaml
var defaultEcsConfig string

//go:embed awsemf_default_prometheus.yaml
var defaultPrometheusConfig string

var (
	ecsBasePathKey        = common.ConfigKey(common.LogsKey, common.MetricsCollectedKey, common.ECSKey)
	eksBasePathKey        = common.ConfigKey(common.LogsKey, common.MetricsCollectedKey, common.KubernetesKey)
	prometheusBasePathKey = common.ConfigKey(common.LogsKey, common.MetricsCollectedKey, common.PrometheusKey)
)

type translator struct {
	factory component.ExporterFactory
}

var _ common.Translator[component.Config] = (*translator)(nil)

func NewTranslator() common.Translator[component.Config] {
	return &translator{awsemfexporter.NewFactory()}
}

func (t *translator) Type() component.Type {
	return t.factory.Type()
}

// Translate creates an awsemf exporter config based on the input json config
func (t *translator) Translate(c *confmap.Conf) (component.Config, error) {
	cfg := t.factory.CreateDefaultConfig().(*awsemfexporter.Config)

	var defaultConfig string
	if t.isEcs(c) {
		defaultConfig = defaultEcsConfig
	} else if t.isEks(c) {
		defaultConfig = defaultEcsConfig // TODO: Fix when onboarding EKS
	} else if t.isPrometheus(c) {
		defaultConfig = defaultPrometheusConfig
	} else {
		return cfg, nil
	}
	if defaultConfig != "" {
		var rawConf map[string]interface{}
		if err := yaml.Unmarshal([]byte(defaultConfig), &rawConf); err != nil {
			return nil, fmt.Errorf("unable to read default config: %w", err)
		}
		conf := confmap.NewFromStringMap(rawConf)
		if err := conf.Unmarshal(&cfg); err != nil {
			return nil, fmt.Errorf("unable to unmarshal config: %w", err)
		}
	}

	// TODO: Do we have use-case of multiple awsemf exporters used in diff pipelines in the same yaml?

	if t.isEcs(c) {
		if err := t.setEcsFields(c, cfg); err != nil {
			return nil, err
		}
	} else if t.isEks(c) {
		if err := t.setEksFields(c, cfg); err != nil {
			return nil, err
		}
	} else if t.isPrometheus(c) {
		if err := t.setPrometheusFields(c, cfg); err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

func (t *translator) isEcs(conf *confmap.Conf) bool {
	return conf.IsSet(ecsBasePathKey)
}

func (t *translator) isEks(conf *confmap.Conf) bool {
	return conf.IsSet(eksBasePathKey)
}

func (t *translator) isPrometheus(conf *confmap.Conf) bool {
	return conf.IsSet(prometheusBasePathKey)
}

func (t *translator) setEcsFields(conf *confmap.Conf, cfg *awsemfexporter.Config) error {
	return nil
}

func (t *translator) setEksFields(conf *confmap.Conf, cfg *awsemfexporter.Config) error {
	return nil
}

func (t *translator) setPrometheusFields(conf *confmap.Conf, cfg *awsemfexporter.Config) error {
	// TODO: clusterName should be part of extension and resource processors. Confirm if all those are addressed

	_, logGroupName := new(prometheus.LogGroupName).ApplyRule(conf.Get(prometheusBasePathKey)) // TODO: remove dependency on rule.
	if logGroupName, ok := logGroupName.(string); ok {
		cfg.LogGroupName = logGroupName
	}

	// logStreamName defaults to {job} for prometheus via our embedded config. We do not respect the log_stream_name field in "logs -> metrics_collected section" for backwards compatibility.
	//
	// We previously used to set the "job" tag on the metric as per https://github.com/aws/private-amazon-cloudwatch-agent-staging/blob/60ca11244badf0cb3ae9dd9984c29f41d7a69302/plugins/inputs/prometheus_scraper/metrics_handler.go#L81-L85
	// And while determining the target, we would give preference to the metric tag over the log_stream_name coming from config/toml as per
	// https://github.com/aws/private-amazon-cloudwatch-agent-staging/blob/60ca11244badf0cb3ae9dd9984c29f41d7a69302/plugins/outputs/cloudwatchlogs/cloudwatchlogs.go#L175-L180.
	//
	// In CCWA, prometheus receiver is going to always set the job label (In the case of ECS, special handling is needed for prometheus_job -> job relabelling as explained in the ecs observer extension translation)
	// Hence, we default the log_stream_name with a placeholder for {job} to achieve backwards compatibility. If we ever come across an edge case where the job label is not set on a metric,
	// we can add a metrics transform processor to insert the job label and set it to "default" i.e. same as https://github.com/aws/private-amazon-cloudwatch-agent-staging/blob/60ca11244badf0cb3ae9dd9984c29f41d7a69302/plugins/inputs/prometheus_scraper/metrics_handler.go#L84

	if conf.IsSet(common.ConfigKey(prometheusBasePathKey, "emf_processor")) {
		_, emfProcessor := new(emfprocessor.EMFProcessor).ApplyRule(conf.Get(prometheusBasePathKey)) // TODO: remove dependency on rule.
		if emfProcessor, ok := emfProcessor.(map[string]interface{}); ok {
			setPrometheusNamespace(emfProcessor, cfg)
			if err := setPrometheusMetricDescriptors(emfProcessor, cfg); err != nil {
				return err
			}
			if err := setPrometheusMetricDeclarations(emfProcessor, cfg); err != nil {
				return err
			}
		}
	}
	return nil
}
