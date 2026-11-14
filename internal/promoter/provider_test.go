/*
Copyright 2026 Ricardo Pinto.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package promoter

import (
	"context"
	"errors"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
)

const (
	paymentsKubeconfig = "acc-payments"
	platformKubeconfig = "acc-platform"
	acceptanceEnv      = "acceptance"
)

func TestSecretClusterName(t *testing.T) {
	t.Parallel()
	opts := KubeconfigOptions{Namespace: "xr-rollout", KubeconfigSecretLabel: KubeconfigSecretLabel}
	secret := &corev1.Secret{}
	secret.Name = "dev"
	secret.Namespace = "xr-rollout"
	secret.Labels = map[string]string{KubeconfigSecretLabel: "true"}
	name, ok := secretClusterName(opts, secret)
	if !ok || name != "dev" {
		t.Fatalf("name = %q, ok = %v", name, ok)
	}

	secret.Namespace = "other"
	if _, ok := secretClusterName(opts, secret); ok {
		t.Fatal("secret in another namespace was selected")
	}

	secret.Namespace = "xr-rollout"
	secret.Labels = nil
	if _, ok := secretClusterName(opts, secret); ok {
		t.Fatal("secret without the label was selected")
	}

	secret.Labels = map[string]string{KubeconfigSecretLabel: "true"}
	secret.Annotations = map[string]string{ClusterAnnotation: toCluster}
	name, ok = secretClusterName(opts, secret)
	if !ok || name != fromCluster {
		t.Fatalf("client id = %q, ok = %v", name, ok)
	}
}

func TestSecretLogicalCluster(t *testing.T) {
	t.Parallel()
	if ClusterAnnotation != "rollouts.platform.example.org/cluster" {
		t.Fatalf("annotation = %s", ClusterAnnotation)
	}
	secret := &corev1.Secret{}
	secret.Name = paymentsKubeconfig
	secret.Annotations = map[string]string{ClusterAnnotation: toCluster}
	opts := KubeconfigOptions{PerSecretClient: true}
	if got := secretLogicalCluster(opts, secret); got != toCluster {
		t.Fatalf("logical = %q", got)
	}
	opts.PerSecretClient = false
	if got := secretLogicalCluster(opts, secret); got != paymentsKubeconfig {
		t.Fatalf("remote logical = %q", got)
	}
	opts.PerSecretClient = true
	secret.Annotations = nil
	if got := secretLogicalCluster(opts, secret); got != paymentsKubeconfig {
		t.Fatalf("unset annotation = %q", got)
	}
}

func TestTwoSecretsShareOneLogicalCluster(t *testing.T) {
	t.Parallel()
	p := NewKubeconfigProvider(KubeconfigOptions{Namespace: "xr-rollout", PerSecretClient: true})
	p.store(paymentsKubeconfig, &engagedCluster{logical: toCluster, environment: acceptanceEnv, cancel: func() {}})
	p.store(platformKubeconfig, &engagedCluster{logical: toCluster, environment: acceptanceEnv, cancel: func() {}})
	if _, err := p.Get(context.Background(), paymentsKubeconfig); err != nil {
		t.Fatalf("payments: %v", err)
	}
	if _, err := p.Get(context.Background(), platformKubeconfig); err != nil {
		t.Fatalf("platform: %v", err)
	}
	if p.clusters[paymentsKubeconfig] == p.clusters[platformKubeconfig] {
		t.Fatal("two secrets share one client")
	}
	names := p.Names()
	if len(names) != 1 || names[0] != toCluster {
		t.Fatalf("names = %v", names)
	}
	if _, err := p.Get(context.Background(), toCluster); err != nil {
		t.Fatalf("logical get: %v", err)
	}
}

func TestParseFailureDoesNotEngage(t *testing.T) {
	t.Parallel()
	p := NewKubeconfigProvider(KubeconfigOptions{Namespace: "xr-rollout", PerSecretClient: true})
	p.store(paymentsKubeconfig, &engagedCluster{logical: toCluster, environment: acceptanceEnv, cancel: func() {}})
	p.rejectKubeconfig(paymentsKubeconfig, "xr-rollout", fmt.Errorf("read kubeconfig: bad"))
	if _, err := p.Get(context.Background(), paymentsKubeconfig); !errors.Is(err, multicluster.ErrClusterNotFound) {
		t.Fatalf("payments get: %v", err)
	}
	if p.CredentialFailure(paymentsKubeconfig) == "" {
		t.Fatal("expected a parse failure")
	}
	p.store(platformKubeconfig, &engagedCluster{logical: toCluster, environment: acceptanceEnv, cancel: func() {}})
	if _, err := p.Get(context.Background(), platformKubeconfig); err != nil {
		t.Fatalf("platform: %v", err)
	}
	names := p.Names()
	if len(names) != 1 || names[0] != toCluster {
		t.Fatalf("names = %v", names)
	}
	env, engaged := p.Engagement(paymentsKubeconfig)
	if engaged || env != "" {
		t.Fatalf("payments engagement = %q, %v", env, engaged)
	}
	env, engaged = p.Engagement(platformKubeconfig)
	if !engaged || env != acceptanceEnv {
		t.Fatalf("platform engagement = %q, %v", env, engaged)
	}
}

func TestRESTConfigFromSecretDoesNotNeedAServer(t *testing.T) {
	t.Parallel()
	cfg, err := restConfigFromKubeconfig([]byte(`
apiVersion: v1
kind: Config
clusters:
- name: dev
  cluster:
    server: https://dev.example:6443
users:
- name: promoter
  user:
    token: not-logged
contexts:
- name: dev
  context:
    cluster: dev
    user: promoter
current-context: dev
`))
	if err != nil {
		t.Fatalf("rest config: %v", err)
	}
	if cfg.Host != "https://dev.example:6443" {
		t.Fatalf("host = %s", cfg.Host)
	}
	if _, err := restConfigFromKubeconfig([]byte("not-yaml")); err == nil {
		t.Fatal("bad kubeconfig returned nil error")
	}
}

func TestEnvironmentAnnotation(t *testing.T) {
	t.Parallel()
	if EnvironmentAnnotation != "rollouts.platform.example.org/environment" {
		t.Fatalf("annotation = %s", EnvironmentAnnotation)
	}
	p := NewKubeconfigProvider(KubeconfigOptions{Namespace: "xr-rollout"})
	if _, ok := p.Environment("acc"); ok {
		t.Fatal("missing cluster reported an environment")
	}
	p.store("acc", &engagedCluster{environment: "acceptance"})
	got, ok := p.Environment("acc")
	if !ok || got != "acceptance" {
		t.Fatalf("Environment = %q, %v", got, ok)
	}
	p.setEnvironment("acc", "")
	if _, ok := p.Environment("acc"); ok {
		t.Fatal("empty annotation reported an environment")
	}
}

func TestSecretEnvironmentAnnotation(t *testing.T) {
	t.Parallel()
	secret := &corev1.Secret{}
	secret.Annotations = map[string]string{EnvironmentAnnotation: "acceptance"}
	if got := secretEnvironment(secret); got != "acceptance" {
		t.Fatalf("secretEnvironment = %q", got)
	}
	secret.Annotations = nil
	if got := secretEnvironment(secret); got != "" {
		t.Fatalf("missing annotation = %q", got)
	}
}
