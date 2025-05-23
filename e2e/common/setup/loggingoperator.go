// Copyright © 2021 Cisco Systems, Inc. and/or its affiliates
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package setup

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	corev1 "k8s.io/api/core/v1"

	"github.com/kube-logging/logging-operator/e2e/common"
)

var (
	defaultImages = []e2eImage{
		{
			lookupEnv:  "LOGGING_OPERATOR_IMAGE",
			repository: "controller",
			tag:        "local",
		},
		{
			lookupEnv:  "CONFIG_RELOADER_IMAGE",
			repository: "config-reloader",
			tag:        "local",
		},
		{
			lookupEnv:  "SYSLOG_NG_RELOADER_IMAGE",
			repository: "syslogng-reload",
			tag:        "local",
		},
		{
			lookupEnv:  "FLUENTD_DRAIN_WATCH_IMAGE",
			repository: "fluentd-drain-watch",
			tag:        "local",
		},
		{
			lookupEnv:  "NODE_EXPORTER_IMAGE",
			repository: "node-exporter",
			tag:        "local",
		},
		{
			lookupEnv:  "FLUENTD_IMAGE",
			repository: "fluentd-full",
			tag:        "local",
		},
	}
)

type e2eImage struct {
	lookupEnv  string
	repository string
	tag        string
}

func (i e2eImage) Format() string {
	return fmt.Sprintf("%s:%s", i.repository, i.tag)
}

func LoggingOperator(t *testing.T, c common.Cluster, opts ...LoggingOperatorOption) {

	opt := &LoggingOperatorOptions{
		Namespace:    "default",
		NameOverride: "logging-operator",
		PollInterval: time.Second * 3,
		Timeout:      time.Minute,
	}

	for _, o := range opts {
		o.ApplyToLoggingOperatorOptions(opt)
	}

	restClientGetter, err := NewRESTClientGetter(c.KubeConfigFilePath(), opt.Namespace)
	if err != nil {
		t.Fatalf("helm rest client getter: %s", err)
	}
	actionConfig := new(action.Configuration)

	err = actionConfig.Init(restClientGetter, opt.Namespace, "memory", func(format string, v ...interface{}) {
		t.Logf(format, v...)
	})

	installer := action.NewInstall(actionConfig)

	installer.Namespace = opt.Namespace
	installer.CreateNamespace = true
	installer.ReleaseName = "logging-operator"

	projectDir := os.Getenv("PROJECT_DIR")
	if projectDir == "" {
		projectDir = "../.."
	}

	cp, err := installer.ChartPathOptions.LocateChart(fmt.Sprintf("%s/charts/logging-operator", projectDir), cli.New())
	if err != nil {
		t.Fatalf("helm locate chart: %s", err)
	}
	chartReq, err := loader.Load(cp)
	if err != nil {
		t.Fatalf("helm load chart: %s", err)
	}

	var loggingOperatorImage e2eImage
	for _, image := range defaultImages {
		if image.lookupEnv == "LOGGING_OPERATOR_IMAGE" {
			loggingOperatorImage = image
		}

		err := c.LoadImages(processImage(t, image))
		if err != nil {
			t.Fatalf("kind load image: %s", err)
		}
	}

	_, err = installer.Run(chartReq, map[string]interface{}{
		"nameOverride": opt.NameOverride,
		"image": map[string]interface{}{
			"repository": loggingOperatorImage.repository,
			"tag":        loggingOperatorImage.tag,
			"pullPolicy": corev1.PullNever,
		},
		"testReceiver": map[string]interface{}{
			"enabled": true,
		},
		"volumes": []map[string]interface{}{
			{
				"name":     "coverage-data",
				"emptyDir": map[string]string{},
			},
		},
		"volumeMounts": []map[string]interface{}{
			{
				"mountPath": "/covdatafiles",
				"name":      "coverage-data",
			},
		},
		"env": []map[string]interface{}{
			{
				"name":  "GOCOVERDIR",
				"value": "/covdatafiles",
			},
		},
		"extraArgs": opt.Args,
	})
	if err != nil {
		t.Fatalf("helm chart install: %s", err)
	}
}

func processImage(t *testing.T, image e2eImage) string {
	imageFromEnv, ok := os.LookupEnv(image.lookupEnv)
	if ok {
		if len(strings.Split(imageFromEnv, ":")) < 2 {
			t.Logf("%s: (%s) is invalid. Using default %s", image.lookupEnv, imageFromEnv, image.Format())
			return image.Format()
		}

		return imageFromEnv
	}
	t.Logf("%s is undefined. Using default %s", image.lookupEnv, image.Format())

	return image.Format()
}

type LoggingOperatorOption interface {
	ApplyToLoggingOperatorOptions(options *LoggingOperatorOptions)
}

type LoggingOperatorOptionFunc func(*LoggingOperatorOptions)

func (fn LoggingOperatorOptionFunc) ApplyToLoggingOperatorOptions(options *LoggingOperatorOptions) {
	fn(options)
}

type LoggingOperatorOptions struct {
	Namespace    string
	NameOverride string
	PollInterval time.Duration
	Timeout      time.Duration
	Args         []string
}
