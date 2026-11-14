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
	// ClusterAnnotation is the logical cluster a kubeconfig Secret targets.
	// Hub mode reads it. When it is absent the Secret name is the cluster name.
	// Remote mode ignores it.
	ClusterAnnotation = "rollouts.platform.example.org/cluster"

	cacheSyncTimeout = 30 * time.Second
)

// KubeconfigOptions selects the Secrets that engage clusters.
type KubeconfigOptions struct {
	Namespace             string
	KubeconfigSecretLabel string
	KubeconfigSecretKey   string
	ClusterOptions        []cluster.Option
	// PerSecretClient engages one client per Secret. The Secret name is the
	// client id. ClusterAnnotation, when set, is the logical cluster.
	// Leave it false for remote mode, where the Secret name is the cluster name.
	PerSecretClient bool
}

// KubeconfigProvider engages one client per labelled kubeconfig Secret.
// Secret creation order is not the promotion order.
// In hub mode several Secrets may share one logical cluster.
type KubeconfigProvider struct {
	opts KubeconfigOptions
	mgr  mcmanager.Manager

	mu       sync.Mutex
	clusters map[string]*engagedCluster
	failures map[string]string
	indexers []fieldIndex
}

type engagedCluster struct {
	cluster     cluster.Cluster
	cancel      context.CancelFunc
	hash        string
	environment string
	logical     string
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
		failures: map[string]string{},
	}
}

// Get returns the client engaged under name.
// name is a Secret name, or a logical cluster when no Secret uses that name.
// An exact Secret name wins. Several Secrets for one logical cluster share
// that cluster for promotion; each Secret stays its own client.
func (p *KubeconfigProvider) Get(_ context.Context, name string) (cluster.Cluster, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if engaged, ok := p.clusters[name]; ok {
		return engaged.cluster, nil
	}
	ids := make([]string, 0, 1)
	for id, engaged := range p.clusters {
		if logicalName(id, engaged) == name {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, multicluster.ErrClusterNotFound
	}
	sort.Strings(ids)
	return p.clusters[ids[0]].cluster, nil
}

// Environment returns the promotion environment annotated on the kubeconfig Secret.
// A missing client or an empty annotation is ("", false).
func (p *KubeconfigProvider) Environment(name string) (string, bool) {
	env, engaged := p.Engagement(name)
	if !engaged || env == "" {
		return "", false
	}
	return env, true
}

// Engagement reports whether client id is engaged and the environment annotation.
// A missing Secret or a kubeconfig that will not parse is engaged=false.
// An engaged Secret with an empty environment annotation is ("", true).
func (p *KubeconfigProvider) Engagement(name string) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	engaged, ok := p.clusters[name]
	if !ok {
		return "", false
	}
	return engaged.environment, true
}

// CredentialFailure returns why client id did not engage, or "" when the
// Secret is only missing.
func (p *KubeconfigProvider) CredentialFailure(name string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.failures[name]
}

func (p *KubeconfigProvider) setEnvironment(name, environment string) {
	p.setMetadata(name, environment, "")
}

func (p *KubeconfigProvider) setMetadata(name, environment, logical string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	engaged, ok := p.clusters[name]
	if !ok {
		return
	}
	engaged.environment = environment
	if logical != "" {
		engaged.logical = logical
	}
}

func (p *KubeconfigProvider) setFailure(name, message string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failures == nil {
		p.failures = map[string]string{}
	}
	p.failures[name] = message
}

func (p *KubeconfigProvider) clearFailure(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.failures, name)
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

// Names returns engaged logical cluster names, sorted for stable logs.
// A cluster is listed once at least one Secret resolves to it.
// Callers must not use this order as the promotion order.
func (p *KubeconfigProvider) Names() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	seen := map[string]struct{}{}
	for id, engaged := range p.clusters {
		seen[logicalName(id, engaged)] = struct{}{}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func logicalName(id string, engaged *engagedCluster) string {
	if engaged.logical != "" {
		return engaged.logical
	}
	return id
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
				p.clearFailure(req.Name)
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get Secret %s: %w", req.NamespacedName, err)
	}

	name, ok := secretClusterName(p.opts, secret)
	if !ok {
		return ctrl.Result{}, nil
	}

	logical := secretLogicalCluster(p.opts, secret)
	env := secretEnvironment(secret)
	raw := secret.Data[p.opts.KubeconfigSecretKey]
	hash := hashKubeconfig(raw)
	if p.sameHash(name, hash) {
		p.setMetadata(name, env, logical)
		p.clearFailure(name)
		return ctrl.Result{}, nil
	}

	cfg, err := restConfigFromKubeconfig(raw)
	if err != nil {
		p.rejectKubeconfig(name, secret.Namespace, err)
		logger.Error(err, "kubeconfig will not parse", "secret", secret.Namespace+"/"+secret.Name)
		return ctrl.Result{}, nil
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

	previous := p.store(name, &engagedCluster{cluster: cl, cancel: cancel, hash: hash, environment: env, logical: logical})
	if previous != nil && previous.cancel != nil {
		previous.cancel()
	}
	p.clearFailure(name)
	logger.Info("engaged cluster", "cluster", logical, "environment", env, "secret", secret.Namespace+"/"+secret.Name)
	return ctrl.Result{}, nil
}

func (p *KubeconfigProvider) rejectKubeconfig(name, namespace string, err error) {
	p.disengage(name)
	p.setFailure(name, fmt.Sprintf("kubeconfig Secret %s/%s will not parse: %v", namespace, name, err))
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
	if engaged.cancel != nil {
		engaged.cancel()
	}
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

func secretLogicalCluster(opts KubeconfigOptions, secret *corev1.Secret) string {
	if opts.PerSecretClient && secret.Annotations != nil {
		if name := secret.Annotations[ClusterAnnotation]; name != "" {
			return name
		}
	}
	return secret.Name
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
