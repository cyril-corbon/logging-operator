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

package common

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"emperror.dev/errors"
	"github.com/spf13/cast"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/cluster"

	"github.com/kube-logging/logging-operator/e2e/common/kind"
)

const defaultClusterName = "e2e-test"

type Cluster interface {
	cluster.Cluster
	LoadImages(images ...string) error
	Cleanup() error
	PrintLogs(config PrintLogConfig) error
}

type PrintLogConfig struct {
	Namespaces []string
	FilePath   string
	Limit      int
}

func WithCluster(t *testing.T, fn func(*testing.T, Cluster), beforeCleanup func(*testing.T, Cluster) error, opts ...cluster.Option) {
	cluster, err := GetTestCluster(defaultClusterName, opts...)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		require.NoError(t, cluster.Start(ctx))
	}()

	defer func() {
		assert.NoError(t, beforeCleanup(t, cluster))
		assert.NoError(t, cluster.Cleanup())
		cancel()
		require.NoError(t, DeleteTestCluster(defaultClusterName))
	}()

	fn(t, cluster)
}

func GetTestCluster(clusterName string, opts ...cluster.Option) (Cluster, error) {
	var err error
	var kubeconfig []byte
	var kubeconfigFile *os.File
	var clientCfg clientcmd.ClientConfig
	var restCfg *rest.Config
	var c cluster.Cluster

	kubeconfig, err = KindClusterKubeconfig(clusterName)
	if err != nil {
		return nil, errors.WrapIfWithDetails(err, "getting kubeconfig of kind cluster", "clusterName", clusterName)
	}
	kubeconfigFile, err = os.CreateTemp("", "kind-kind-kubeconfig")
	if err != nil {
		return nil, errors.WrapIfWithDetails(err, "unable to create temp file for kubeconfig", "clusterName", clusterName)
	}
	err = os.WriteFile(kubeconfigFile.Name(), kubeconfig, os.FileMode(0600))
	if err != nil {
		return nil, errors.WrapIfWithDetails(err, "failed to write kubeconfig", "clusterName", clusterName, "path", kubeconfigFile.Name())
	}
	clientCfg, err = clientcmd.NewClientConfigFromBytes(kubeconfig)
	if err != nil {
		return nil, errors.WrapIfWithDetails(err, "creating client config from kubeconfig bytes", "kubeconfig", kubeconfig)
	}
	restCfg, err = clientCfg.ClientConfig()
	if err != nil {
		return nil, errors.WrapIfWithDetails(err, "creating rest config from client config", "cfg", clientCfg)
	}
	c, err = cluster.New(restCfg, opts...)
	return &kindCluster{
		Cluster:            c,
		kubeconfigFilePath: kubeconfigFile.Name(),
		clusterName:        clusterName,
	}, errors.WrapIfWithDetails(err, "creating cluster with rest config", "cfg", restCfg)
}

func DeleteTestCluster(clusterName string) error {
	return errors.WrapIfWithDetails(kind.DeleteCluster(kind.DeleteClusterOptions{
		Name: clusterName,
	}), "deleting kind cluster", "clusterName", clusterName)
}

type kindCluster struct {
	cluster.Cluster
	kubeconfigFilePath string
	clusterName        string
}

func (c kindCluster) PrintLogs(config PrintLogConfig) error {
	cmd := exec.Command("stern", "-n", strings.Join(config.Namespaces, ","), ".*", "--no-follow", "--tail", cast.ToString(config.Limit), "--kubeconfig", c.kubeconfigFilePath)
	f, err := os.Create(config.FilePath)
	if err != nil {
		return err
	}
	defer f.Close()

	cmd.Stdout = f
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func (c kindCluster) Cleanup() error {
	return os.Remove(c.kubeconfigFilePath)
}

func (c kindCluster) LoadImages(images ...string) error {
	return kind.LoadDockerImage(images, kind.LoadDockerImageOptions{
		Name: c.clusterName,
	})
}
