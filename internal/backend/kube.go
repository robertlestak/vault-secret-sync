package backend

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"github.com/hashicorp/vault/sdk/logical"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/runtime"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"

	"github.com/robertlestak/vault-secret-sync/api/v1alpha1"
	vaultv1alpha1 "github.com/robertlestak/vault-secret-sync/api/v1alpha1"
	zzap "go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

type SyncStatusString string

const (
	SyncStatusInit    SyncStatusString = "Initialized"
	SyncStatusSuccess SyncStatusString = "Synced"
	SyncStatusFailed  SyncStatusString = "Failed"
	SyncStatusDryRun  SyncStatusString = "DryRun"
)

var (
	Scheme     = runtime.NewScheme()
	setupLog   = ctrl.Log.WithName("setup")
	Reconciler *VaultSecretSyncReconciler
)

type KubernetesBackend struct {
	MetricsAddr             string `yaml:"metricsAddr" json:"metricsAddr"`
	EnableLeaderElection    bool   `yaml:"enableLeaderElection" json:"enableLeaderElection"`
	LeaderElectionNamespace string `yaml:"leaderElectionNamespace" json:"leaderElectionNamespace"`
	LeaderElectionID        string `yaml:"leaderElectionId" json:"leaderElectionId"`
}

func init() {
	utilruntime.Must(scheme.AddToScheme(Scheme))
	utilruntime.Must(vaultv1alpha1.AddToScheme(Scheme))
}

type VaultSecretSyncReconciler struct {
	client.Client
	APIReader client.Reader
	Scheme    *runtime.Scheme
	Recorder  record.EventRecorder
}

func (b *KubernetesBackend) Type() BackendType {
	return BackendTypeKubernetes
}

func SetSyncStatus(ctx context.Context, sc v1alpha1.VaultSecretSync, status SyncStatusString) error {
	if B == nil {
		return nil
	}
	switch B.Type() {
	case BackendTypeKubernetes:
		return setSyncStatusKube(ctx, sc, status)
	default:
		return nil
	}
}

func WriteEvent(ctx context.Context, namespace string, name string, Event string, reason string, message string) error {
	if Reconciler == nil {
		return nil
	}
	s := &vaultv1alpha1.VaultSecretSync{}
	err := Reconciler.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, s)
	if err != nil {
		return err
	}
	Reconciler.Recorder.Event(s, Event, reason, message)
	return nil
}

// AnnotationOperations handles annotations on VaultSecretSync objects
func AnnotationOperations(r *VaultSecretSyncReconciler, vaultSecretSync *v1alpha1.VaultSecretSync) error {
	l := log.WithFields(log.Fields{
		"action": "annotationOperations",
	})
	l.Trace("start")
	defer l.Trace("end")

	// If it has a "force-sync" annotation, trigger a manual sync and then remove the annotation so it doesn't trigger again
	if vaultSecretSync.ObjectMeta.Annotations["force-sync"] != "" {
		l.Debug("sync annotation found, triggering force-sync sync")

		// Look for an operation annotation, if we don't find one, default to update
		var op logical.Operation
		if vaultSecretSync.ObjectMeta.Annotations["op"] != "" {
			l.Debugf("operation annotation found: %s", vaultSecretSync.ObjectMeta.Annotations["op"])
			op = logical.Operation(vaultSecretSync.ObjectMeta.Annotations["op"])
		} else {
			op = logical.UpdateOperation
		}
		l.Debugf("operation: %s", op)
		if err := ManualTrigger(context.Background(), *vaultSecretSync, op); err != nil {
			r.Recorder.Event(vaultSecretSync, "Warning", "ManualTrigger", "Failed to trigger force-sync sync")
			return err
		}
		l.Debug("force-sync sync triggered")

		// Delete the annotations after triggering sync
		delete(vaultSecretSync.ObjectMeta.Annotations, "force-sync")
		delete(vaultSecretSync.ObjectMeta.Annotations, "op")

		// Update the VaultSecretSync object
		if err := r.Update(context.Background(), vaultSecretSync, client.FieldOwner("vault-secret-sync-controller")); err != nil {
			l.Errorf("failed to update object: %v", err)
			return err
		}
		r.Recorder.Event(vaultSecretSync, "Normal", "ManualTrigger", "Force-sync sync triggered")
	}
	l.Debug("annotation operations complete")
	return nil
}

func createHash(s vaultv1alpha1.VaultSecretSync) (string, error) {
	l := log.WithFields(log.Fields{
		"action": "createHash",
	})
	l.Trace("start")
	defer l.Trace("end")
	specBytes, err := json.Marshal(s.Spec)
	if err != nil {
		return "", fmt.Errorf("failed to marshal spec: %v", err)
	}
	l.Debugf("specBytes: %s", specBytes)
	hash := sha256.New()
	_, err = hash.Write(specBytes)
	if err != nil {
		return "", fmt.Errorf("failed to write hash: %v", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func setSyncStatusKube(ctx context.Context, sc v1alpha1.VaultSecretSync, status SyncStatusString) error {
	l := log.WithFields(log.Fields{
		"action":    "setSyncStatusKube",
		"status":    status,
		"namespace": sc.Namespace,
		"name":      sc.Name,
	})
	l.Trace("start")
	defer l.Trace("end")
	l.Debug("setting sync status")
	s := &vaultv1alpha1.VaultSecretSync{}
	err := Reconciler.Get(ctx, client.ObjectKey{Namespace: sc.Namespace, Name: sc.Name}, s)
	if err != nil {
		l.Errorf("failed to get object: %v", err)
		return err
	}
	objHash, err := createHash(*s)
	if err != nil {
		l.Errorf("failed to create hash: %v", err)
		return err
	}
	// Prepare the patch
	s.Status.Status = string(status)
	s.Status.LastSyncTime = metav1.Now()
	s.Status.SyncDestinations = len(s.Spec.Dest)
	s.Status.Hash = objHash
	l.Debugf("updating status: %+v", s.Status)
	if err := Reconciler.Status().Update(context.Background(), s, client.FieldOwner("vault-secret-sync-controller")); err != nil {
		l.Errorf("failed to update status: %v", err)
		return err
	}
	l.Debug("status updated")
	return nil
}

func (r *VaultSecretSyncReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.WithFields(log.Fields{
		"action": "Reconcile",
	})
	l.Trace("start")
	defer l.Trace("end")
	_ = ctrl.Log.WithName("controllers").WithName("VaultSecretSync")

	l = l.WithFields(log.Fields{
		"namespace": req.Namespace,
		"name":      req.Name,
	})
	l.Debug("reconciling VaultSecretSync")

	// Fetch the VaultSecretSync instance
	vaultSecretSync := &vaultv1alpha1.VaultSecretSync{}
	err := r.Get(ctx, req.NamespacedName, vaultSecretSync)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		l.Trace("object not found")
		internalName := InternalName(req.Namespace, req.Name)
		if err := RemoveSyncConfig(internalName); err != nil {
			l.Errorf("failed to remove sync config: %v", err)
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	l = l.WithFields(log.Fields{
		"namespace": vaultSecretSync.Namespace,
		"name":      vaultSecretSync.Name,
	})
	l.Trace("retrieved VaultSecretSync")

	// Check if the object is being deleted
	if !vaultSecretSync.ObjectMeta.DeletionTimestamp.IsZero() {
		// The object is being deleted
		if err := RemoveSyncConfig(InternalName(vaultSecretSync.Namespace, vaultSecretSync.Name)); err != nil {
			l.Errorf("failed to remove sync config: %v", err)
		}
		if vaultSecretSync.ObjectMeta.Annotations["delete-on-removal"] == "true" {
			if err := ManualTrigger(ctx, *vaultSecretSync, logical.DeleteOperation); err != nil {
				r.Recorder.Event(vaultSecretSync, "Warning", "Deleting", "Failed to delete secret")
			}
		}
		r.Recorder.Event(vaultSecretSync, "Normal", "Deleted", "Finalizer operations completed and finalizer removed")
		return ctrl.Result{}, nil
	}

	// sanity check debug log the input object
	l.Debugf("VaultSecretSync.Spec.Source: %+v VaultSecretSync.Spec.Dest: %+v", vaultSecretSync.Spec.Source, vaultSecretSync.Spec.Dest)
	var syncNow bool
	// Check if the object has been initialized
	if vaultSecretSync.Status.Status == "" {
		l.Debug("initializing object")
		syncNow = true
	}

	// check if the number of destinations has changed
	if vaultSecretSync.Status.SyncDestinations != len(vaultSecretSync.Spec.Dest) {
		l.Debug("number of destinations has changed")
		syncNow = true
	}
	objHash, err := createHash(*vaultSecretSync)
	if err != nil {
		l.Errorf("failed to create hash: %v", err)
		return ctrl.Result{}, err
	}
	if objHash != vaultSecretSync.Status.Hash {
		l.WithFields(log.Fields{
			"oldHash": vaultSecretSync.Status.Hash,
			"newHash": objHash,
		}).Debug("hash has changed")
		syncNow = true
	}

	// Check if the object has been synced
	if err := AddSyncConfig(*vaultSecretSync); err != nil {
		l.Errorf("failed to add sync config: %v", err)
		return ctrl.Result{}, err
	}

	if syncNow {
		l.Debug("syncing now")
		// trigger a sync on creation
		if err := ManualTrigger(ctx, *vaultSecretSync, logical.UpdateOperation); err != nil {
			r.Recorder.Event(vaultSecretSync, "Warning", "Created", "Failed to trigger initial sync")
		}
	}

	if err := AnnotationOperations(r, vaultSecretSync); err != nil {
		l.Errorf("failed to process annotations: %v", err)
		return ctrl.Result{}, err
	}

	l.Debug("reconcile complete")

	return ctrl.Result{}, nil
}

func (r *VaultSecretSyncReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vaultv1alpha1.VaultSecretSync{}).
		Complete(r)
}

func NewKubernetesBackend() *KubernetesBackend {
	return &KubernetesBackend{}
}

func ctrlLogger() {
	var zo []zap.Opts
	// if the log level is debug or trace, set the controller-runtime logger to debug
	if log.GetLevel() == log.DebugLevel || log.GetLevel() == log.TraceLevel {
		zo = append(zo, zap.UseDevMode(true))
	}
	// convert logrus level to zap level
	switch log.GetLevel() {
	case log.TraceLevel:
		zo = append(zo, zap.Level(zzap.DebugLevel))
	case log.DebugLevel:
		zo = append(zo, zap.Level(zzap.DebugLevel))
	case log.InfoLevel:
		zo = append(zo, zap.Level(zzap.InfoLevel))
	case log.WarnLevel:
		zo = append(zo, zap.Level(zzap.WarnLevel))
	case log.ErrorLevel:
		zo = append(zo, zap.Level(zzap.ErrorLevel))
	case log.FatalLevel:
		zo = append(zo, zap.Level(zzap.FatalLevel))
	case log.PanicLevel:
		zo = append(zo, zap.Level(zzap.PanicLevel))
	}
	zl := zap.New(zo...)
	ctrl.SetLogger(zl)
}

func (b *KubernetesBackend) setupOperator(ctx context.Context) error {
	l := log.WithFields(log.Fields{
		"action":  "setupOperator",
		"pkg":     "backend",
		"backend": "kubernetes",
	})
	l.Trace("start")
	ctrlLogger()
	b.MetricsAddr = cmp.Or(b.MetricsAddr, ":9080")
	b.LeaderElectionID = cmp.Or(b.LeaderElectionID, "vault-secret-sync-leader-election")
	opts := ctrl.Options{
		Scheme: Scheme,
		Metrics: server.Options{
			BindAddress: b.MetricsAddr,
		},
		LeaderElection:   b.EnableLeaderElection,
		LeaderElectionID: b.LeaderElectionID,
	}
	if b.LeaderElectionNamespace != "" {
		opts.LeaderElectionNamespace = b.LeaderElectionNamespace
	}
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), opts)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		return err
	}
	reconciler := &VaultSecretSyncReconciler{
		Client:    mgr.GetClient(),
		APIReader: mgr.GetAPIReader(),
		Scheme:    mgr.GetScheme(),
		Recorder:  mgr.GetEventRecorderFor("vault-secret-sync-controller"),
	}
	if err = reconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VaultSecretSync")
		return err
	}
	Reconciler = reconciler

	setupLog.Info("starting manager")
	l.Info("starting manager")
	go func(ctx context.Context) {
		if err := mgr.Start(ctx); err != nil {
			setupLog.Error(err, "problem running manager")
			os.Exit(1)
		}
	}(ctx)
	return nil
}

func (b *KubernetesBackend) Start(ctx context.Context, params map[string]any) error {
	jd, err := json.Marshal(params)
	if err != nil {
		return err
	}
	err = json.Unmarshal(jd, b)
	if err != nil {
		return err
	}
	return b.setupOperator(ctx)
}
