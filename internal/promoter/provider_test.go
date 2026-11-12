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
	"testing"

	corev1 "k8s.io/api/core/v1"
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
