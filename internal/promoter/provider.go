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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/log"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
)

const (
	// KubeconfigSecretLabel marks a Secret whose data is a kubeconfig.
	KubeconfigSecretLabel = "sigs.k8s.io/multicluster-runtime-kubeconfig"
	// KubeconfigSecretKey is the data key that holds the kubeconfig bytes.
	KubeconfigSecretKey = "kubeconfig"
	// EnvironmentAnnotation is the per-cluster promotion environment.
	// It is not the Secret name. Cluster acc may be annotated acceptance.
	EnvironmentAnnotation = "rollouts.platform.example.org/environment"

	cacheSyncTimeout = 30 * time.Second
)

// KubeconfigOptions selects the Secrets that engage clusters.
type KubeconfigOptions struct {
	Namespace             string
	KubeconfigSecretLabel string
	KubeconfigSecretKey   string
	ClusterOptions        []cluster.Option
}

// KubeconfigProvider engages one cluster per labelled kubeconfig Secret.
// Secret creation order is not the promotion order.
type KubeconfigProvider struct {
	opts KubeconfigOptions
	mgr  mcmanager.Manager

	mu       sync.Mutex
	clusters map[string]*engagedCluster
	indexers []fieldIndex
}

type engagedCluster struct {
	cluster     cluster.Cluster
	cancel      context.CancelFunc
	hash        string
	environment string
}

type fieldIndex struct {
	obj     client.Object
	field   string
	extract client.IndexerFunc
}

// NewKubeconfigProvider returns a provider that watches kubeconfig Secrets.
func NewKubeconfigProvider(opts KubeconfigOptions) *KubeconfigProvider {
	if opts.KubeconfigSecretLabel == "" {
		opts.KubeconfigSecretLabel = KubeconfigSecretLabel
	}
	if opts.KubeconfigSecretKey == "" {
		opts.KubeconfigSecretKey = KubeconfigSecretKey
	}
	return &KubeconfigProvider{
		opts:     opts,
		clusters: map[string]*engagedCluster{},
	}
}

// Get returns an engaged cluster, or ErrClusterNotFound.
func (p *KubeconfigProvider) Get(_ context.Context, clusterName string) (cluster.Cluster, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	engaged, ok := p.clusters[clusterName]
	if !ok {
		return nil, multicluster.ErrClusterNotFound
	}
	return engaged.cluster, nil
}

// Environment returns the promotion environment annotated on the kubeconfig Secret.
// A missing cluster or an empty annotation is ("", false).
func (p *KubeconfigProvider) Environment(clusterName string) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	engaged, ok := p.clusters[clusterName]
	if !ok || engaged.environment == "" {
		return "", false
	}
	return engaged.environment, true
}

func (p *KubeconfigProvider) setEnvironment(name, environment string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	engaged, ok := p.clusters[name]
	if !ok {
		return
	}
	engaged.environment = environment
}

// IndexField records an indexer and applies it to clusters already engaged.
func (p *KubeconfigProvider) IndexField(ctx context.Context, obj client.Object, field string, extractValue client.IndexerFunc) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.indexers = append(p.indexers, fieldIndex{obj: obj, field: field, extract: extractValue})
	for name, engaged := range p.clusters {
		if err := engaged.cluster.GetFieldIndexer().IndexField(ctx, obj, field, extractValue); err != nil {
			return fmt.Errorf("index %s on cluster %s: %w", field, name, err)
		}
	}
	return nil
}

// SetupWithManager registers the Secret controller on the local manager.
// Call it before the manager starts.
func (p *KubeconfigProvider) SetupWithManager(mgr mcmanager.Manager) error {
	p.mgr = mgr
	return ctrl.NewControllerManagedBy(mgr.GetLocalManager()).
		Named("xr-rollout-kubeconfigs").
		For(&corev1.Secret{}).
		Complete(p)
}

// Names returns engaged cluster names, sorted for stable logs.
// Callers must not use this order as the promotion order.
func (p *KubeconfigProvider) Names() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	names := make([]string, 0, len(p.clusters))
	for name := range p.clusters {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Reconcile engages, replaces, or forgets the cluster named by a kubeconfig Secret.
func (p *KubeconfigProvider) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	secret := &corev1.Secret{}
	if err := p.mgr.GetLocalManager().GetClient().Get(ctx, req.NamespacedName, secret); err != nil {
		if apierrors.IsNotFound(err) {
			if req.Namespace == p.opts.Namespace {
				if p.disengage(req.Name) {
					logger.Info("disengaged cluster", "cluster", req.Name, "secret", req.Namespace+"/"+req.Name)
				}
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get Secret %s: %w", req.NamespacedName, err)
	}

	name, ok := secretClusterName(p.opts, secret)
	if !ok {
		return ctrl.Result{}, nil
	}

	env := secretEnvironment(secret)
	raw := secret.Data[p.opts.KubeconfigSecretKey]
	hash := hashKubeconfig(raw)
	if p.sameHash(name, hash) {
		p.setEnvironment(name, env)
		return ctrl.Result{}, nil
	}

	cfg, err := restConfigFromKubeconfig(raw)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("parse kubeconfig for cluster %s: %w", name, err)
	}
	cl, err := cluster.New(cfg, p.opts.ClusterOptions...)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("build cluster %s: %w", name, err)
	}
	if err := p.applyIndexers(ctx, cl); err != nil {
		return ctrl.Result{}, fmt.Errorf("index cluster %s: %w", name, err)
	}

	// The reconcile context ends when this call returns. The cluster lives
	// until the Secret is removed or its kubeconfig bytes change.
	clusterCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	startErr := make(chan error, 1)
	go func() {
		startErr <- cl.Start(clusterCtx)
	}()

	syncCtx, syncCancel := context.WithTimeout(clusterCtx, cacheSyncTimeout)
	synced := cl.GetCache().WaitForCacheSync(syncCtx)
	syncCancel()
	select {
	case err := <-startErr:
		cancel()
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("start cluster %s: %w", name, err)
		}
		return ctrl.Result{}, fmt.Errorf("start cluster %s: stopped before engage", name)
	default:
	}
	if !synced {
		cancel()
		return ctrl.Result{}, fmt.Errorf("sync cache for cluster %s", name)
	}
	if err := p.mgr.Engage(clusterCtx, name, cl); err != nil {
		cancel()
		return ctrl.Result{}, fmt.Errorf("engage cluster %s: %w", name, err)
	}

	previous := p.store(name, &engagedCluster{cluster: cl, cancel: cancel, hash: hash, environment: env})
	if previous != nil {
		previous.cancel()
	}
	logger.Info("engaged cluster", "cluster", name, "environment", env, "secret", secret.Namespace+"/"+secret.Name)
	return ctrl.Result{}, nil
}

func (p *KubeconfigProvider) sameHash(name, hash string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	engaged, ok := p.clusters[name]
	return ok && engaged.hash == hash
}

func (p *KubeconfigProvider) applyIndexers(ctx context.Context, cl cluster.Cluster) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, index := range p.indexers {
		if err := cl.GetFieldIndexer().IndexField(ctx, index.obj, index.field, index.extract); err != nil {
			return fmt.Errorf("index %s: %w", index.field, err)
		}
	}
	return nil
}

func (p *KubeconfigProvider) store(name string, engaged *engagedCluster) *engagedCluster {
	p.mu.Lock()
	defer p.mu.Unlock()
	previous := p.clusters[name]
	p.clusters[name] = engaged
	return previous
}

func (p *KubeconfigProvider) disengage(name string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	engaged, ok := p.clusters[name]
	if !ok {
		return false
	}
	engaged.cancel()
	delete(p.clusters, name)
	return true
}

func secretEnvironment(secret *corev1.Secret) string {
	if secret.Annotations == nil {
		return ""
	}
	return secret.Annotations[EnvironmentAnnotation]
}

func secretClusterName(opts KubeconfigOptions, secret *corev1.Secret) (string, bool) {
	if secret.Namespace != opts.Namespace || secret.Name == "" {
		return "", false
	}
	label := opts.KubeconfigSecretLabel
	if label == "" {
		label = KubeconfigSecretLabel
	}
	if secret.Labels[label] != "true" {
		return "", false
	}
	return secret.Name, true
}

func restConfigFromKubeconfig(kubeconfig []byte) (*rest.Config, error) {
	cfg, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("read kubeconfig: %w", err)
	}
	return cfg, nil
}

func hashKubeconfig(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
