package sync

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/vault/sdk/logical"
	"github.com/robertlestak/vault-secret-sync/api/v1alpha1"
	"github.com/robertlestak/vault-secret-sync/internal/backend"
	"github.com/robertlestak/vault-secret-sync/internal/event"
	"github.com/robertlestak/vault-secret-sync/internal/metrics"
	"github.com/robertlestak/vault-secret-sync/internal/queue"
	"github.com/robertlestak/vault-secret-sync/internal/transforms"
	"github.com/robertlestak/vault-secret-sync/pkg/driver"
	log "github.com/sirupsen/logrus"
)

func strippedPath(p string) string {
	parts := strings.Split(p, "/")
	if len(parts) < 2 {
		return p
	}
	if parts[1] == "data" || parts[1] == "metadata" {
		return path.Join(parts[0], path.Join(parts[2:]...))
	}
	return p
}

// isPathMatch checks if the event path matches the configuration path
func isPathMatch(configPath, eventPath string) bool {
	if isExactPathMatch(configPath, eventPath) {
		return true
	}
	if isRegexPathMatch(configPath, eventPath) {
		return true
	}
	return false
}

// isExactPathMatch checks if the event path matches the configuration path exactly
func isExactPathMatch(configPath, eventPath string) bool {
	return configPath == eventPath
}

// isRegexPathMatch checks if the event path matches the configuration path using regex
func isRegexPathMatch(configPath, eventPath string) bool {
	if isRegexPath(configPath) {
		fullPattern := fmt.Sprintf("^%s$", configPath)
		rx, err := regexp.Compile(fullPattern)
		if err != nil {
			log.WithFields(log.Fields{
				"action":      "isRegexPathMatch",
				"error":       err,
				"configPath":  configPath,
				"fullPattern": fullPattern,
				"eventPath":   eventPath,
			}).Error("failed to compile regex")
			return false
		}
		return rx.MatchString(eventPath)
	}
	return false
}

// insertSliceString inserts a value into a slice at a specified index
func insertSliceString(a []string, index int, value string) []string {
	if len(a) == index {
		return append(a, value)
	}
	a = append(a[:index+1], a[index:]...)
	a[index] = value
	return a
}

// countRegexMatches counts the number of regex matches across all destinations
func countRegexMatches(ctx context.Context, sc *SyncClients) (int, error) {
	l := log.WithFields(log.Fields{"action": "countRegexMatches"})
	l.Trace("start")

	highestNonRegexPath := findHighestNonRegexPath(sc.Source.GetPath())
	list, err := LoopWildcardRecursive(ctx, sc.Source, highestNonRegexPath)
	if err != nil {
		return 0, err
	}

	rx, err := regexp.Compile(sc.Source.GetPath())
	if err != nil {
		return 0, err
	}

	matchCount := 0
	for _, p := range list {
		if rx.MatchString(p) {
			matchCount += len(sc.Dest)
		}
	}

	l.WithField("matchCount", matchCount).Debug("counted regex matches")
	return matchCount, nil
}

// isRegexPath determines if the provided string is a regex or a literal path
func isRegexPath(path string) bool {
	if !strings.ContainsAny(path, "[](){}+*?|") {
		return false
	}
	_, err := regexp.Compile(path)
	return err == nil
}

// findHighestNonRegexPath finds the highest non-regex path
func findHighestNonRegexPath(p string) string {
	parts := strings.Split(p, "/")
	highestNonRegexPath := ""
	for _, part := range parts {
		if isRegexPath(part) {
			break
		}
		if highestNonRegexPath == "" {
			highestNonRegexPath = part
		} else {
			highestNonRegexPath = path.Join(highestNonRegexPath, part)
		}
	}
	return highestNonRegexPath
}

func ScheduleSync(ctx context.Context, e event.VaultEvent) error {
	l := log.WithFields(log.Fields{
		"action":  "ScheduleSync",
		"eventId": e.ID,
		"op":      e.Operation,
		"path":    e.Path,
		"tenant":  e.Address,
	})
	if e.Namespace != "" {
		l = l.WithField("namespace", e.Namespace)
	}

	if e.ID == "" {
		e.ID = uuid.New().String()
		l = l.WithField("eventId", e.ID)
	}
	l.Trace("start")
	defer l.Trace("end")
	if err := queue.Q.Publish(ctx, e); err != nil {
		l.Error(err)
		return err
	}
	return nil
}

func hasCaptureGroups(regex string) bool {
	re := regexp.MustCompile(`\([^?][^)]*\)`)
	return re.MatchString(regex)
}

func CreateOne(ctx context.Context, j SyncJob, source, dest SyncClient, sourcePath, destPath string) error {
	l := log.WithFields(log.Fields{
		"action":      "syncCreate",
		"source.Path": sourcePath,
		"dest.Path":   destPath,
	})
	l.Trace("start")
	defer l.Trace("end")

	if sourcePath == "" || destPath == "" {
		l.Error("sourcePath or destPath is empty")
		return errors.New("source path and destination path required")
	}

	if shouldFilterSecret(j, sourcePath, destPath) {
		return nil
	}

	if j.SyncConfig.Spec.DryRun != nil && *j.SyncConfig.Spec.DryRun {
		l.Info("dry run")
		return nil
	}

	l.Debug("syncing secret")

	ssecret, serr := source.GetSecret(ctx, sourcePath)
	if serr != nil {
		return handleCreateOneError(ctx, serr, j, dest, sourcePath, destPath)
	}

	ssecret, serr = transforms.ExecuteTransforms(j.SyncConfig, ssecret)
	if serr != nil {
		return handleCreateOneError(ctx, serr, j, dest, sourcePath, destPath)
	}

	if shouldDryRun(j, dest, sourcePath, destPath) {
		return nil
	}

	_, werr := dest.WriteSecret(ctx, j.SyncConfig.ObjectMeta, destPath, ssecret)
	if werr != nil {
		return handleCreateOneError(ctx, werr, j, dest, sourcePath, destPath)
	}

	return handleCreateOneSuccess(ctx, j, dest, sourcePath, destPath)
}

func handleCreateOneError(ctx context.Context, err error, j SyncJob, dest SyncClient, sourcePath, destPath string) error {
	l := log.WithFields(log.Fields{"action": "handleCreateOneError", "error": err})
	l.Error("failed to sync secret")
	backend.WriteEvent(
		ctx,
		j.SyncConfig.Namespace,
		j.SyncConfig.Name,
		"Warning",
		string(backend.SyncStatusFailed),
		fmt.Sprintf("failed to sync %s to %s: %s with error: %s", sourcePath, dest.Driver(), destPath, err.Error()),
	)
	return err
}

func handleCreateOneSuccess(ctx context.Context, j SyncJob, dest SyncClient, sourcePath, destPath string) error {
	l := log.WithFields(log.Fields{"action": "handleCreateOneSuccess"})
	l.Trace("end")
	backend.WriteEvent(
		ctx,
		j.SyncConfig.Namespace,
		j.SyncConfig.Name,
		"Normal",
		string(backend.SyncStatusSuccess),
		fmt.Sprintf("synced %s to %s: %s", sourcePath, dest.Driver(), destPath),
	)
	return nil
}

func LoopWildcardRecursive(ctx context.Context, source SyncClient, sourcePath string) ([]string, error) {
	l := log.WithFields(log.Fields{"action": "LoopWildcardRecursive"})
	l.Trace("start")

	var fullList []string
	sp := findHighestNonRegexPath(sourcePath)
	list, err := source.ListSecrets(ctx, sp)
	if err != nil && strings.Contains(err.Error(), "secret path must be in kv/path/to/secret format") {
		return []string{sp}, nil
	}
	if err != nil {
		l.Error(err)
		return fullList, err
	}

	for _, v := range list {
		nsp := path.Join(sp, v)
		if strings.HasSuffix(v, "/") {
			// prevent infinite loop
			if nsp == sourcePath {
				break
			}
			pl, err := LoopWildcardRecursive(ctx, source, nsp)
			if err != nil {
				l.Error(err)
				return fullList, err
			}
			fullList = append(fullList, pl...)
		} else {
			fullList = append(fullList, nsp)
		}
	}
	l.Trace("end")
	return fullList, nil
}

func NeedsSync(sc v1alpha1.VaultSecretSync, evt event.VaultEvent) bool {
	l := log.WithFields(log.Fields{
		"action":     "NeedsSync",
		"eventPath":  evt.Path,
		"eventOp":    evt.Operation,
		"eventVault": evt.Address,
	})

	if sc.Spec.Suspend != nil && *sc.Spec.Suspend {
		l.Trace("sync suspended")
		return false
	}

	if evt.SyncName != "" && backend.InternalName(sc.Namespace, sc.Name) != evt.SyncName {
		l.Trace("no sync name match")
		return false
	}

	if sc.Spec.Source == nil {
		l.Warn("source is not defined")
		return false
	}

	if evt.Address != sc.Spec.Source.Address {
		l.Tracef("no vault addr match. %s != %s", evt.Address, sc.Spec.Source.Address)
		return false
	}

	sourceNs := sc.Spec.Source.Namespace
	checkEventNs := strings.TrimRight(evt.Namespace, "/")
	if checkEventNs != "" && sourceNs != "" && checkEventNs != sourceNs {
		l.Tracef("no namespace match. %s != %s", checkEventNs, sourceNs)
		return false
	}

	if evt.Operation == logical.DeleteOperation && (sc.Spec.SyncDelete != nil && !*sc.Spec.SyncDelete) {
		l.Trace("delete operation not allowed")
		return false
	}

	sourcePath := sc.Spec.Source.Path
	ss := strings.Split(sourcePath, "/")
	ms := ss
	ss = insertSliceString(ss, 1, "data")
	ms = insertSliceString(ms, 1, "metadata")
	dp := strings.Join(ss, "/")
	mp := strings.Join(ms, "/")

	l = l.WithFields(log.Fields{
		"dataPath":     dp,
		"metadataPath": mp,
		"checkPath":    evt.Path,
	})
	l.Trace("checking path")

	if isPathMatch(sourcePath, evt.Path) || isPathMatch(dp, evt.Path) || isPathMatch(mp, evt.Path) {
		l.Debug("found source, needs sync")
		return true
	}

	l.Trace("no match")
	return false
}

func ManualTrigger(ctx context.Context, cfg v1alpha1.VaultSecretSync, op logical.Operation) error {
	l := log.WithFields(log.Fields{"action": "ManualTrigger"})
	l.Trace("start")
	defer l.Trace("end")

	name := backend.InternalName(cfg.Namespace, cfg.Name)
	l = l.WithFields(log.Fields{"name": name})
	l.Debug("manual trigger")
	evt := event.VaultEvent{
		SyncName:  name,
		Operation: op,
		Manual:    true,
	}
	return queue.Q.Push(evt)
}

func countDeleteRegexMatches(ctx context.Context, sc *SyncClients, j SyncJob) (int, error) {
	l := log.WithFields(log.Fields{"action": "countDeleteRegexMatches"})
	l.Trace("start")

	highestNonRegexPath := findHighestNonRegexPath(sc.Source.GetPath())
	list, err := LoopWildcardRecursive(ctx, sc.Source, highestNonRegexPath)
	if err != nil {
		return 0, err
	}

	rx, err := regexp.Compile(sc.Source.GetPath())
	if err != nil {
		return 0, err
	}

	matchCount := 0
	for _, p := range list {
		if rx.MatchString(p) {
			matchCount += len(sc.Dest)
		}
	}

	l.WithField("matchCount", matchCount).Debug("counted regex matches")
	return matchCount, nil
}

func doSync(ctx context.Context, j SyncJob) error {
	l := log.WithFields(log.Fields{"action": "sync", "name": j.SyncConfig.Name, "namespace": j.SyncConfig.Namespace})
	l.Trace("start")
	defer l.Trace("end")
	startTime := time.Now()
	metrics.SyncsTotal.WithLabelValues(j.SyncConfig.Namespace, j.SyncConfig.Name).Inc()
	metrics.ActiveSyncs.WithLabelValues(j.SyncConfig.Namespace, j.SyncConfig.Name).Inc()

	scs, err := clientGenerator(ctx, j)
	if err != nil {
		return handleSyncError(ctx, err, j, startTime)
	}
	if scs == nil || scs.Source == nil || scs.Dest == nil {
		return handleSyncError(ctx, errors.New("failed to create clients"), j, startTime)
	}
	defer scs.CloseClients(ctx)
	switch j.VaultEvent.Operation {
	case logical.CreateOperation, logical.UpdateOperation:
		l.Trace("create operation")
		err = SyncCreate(ctx, scs, j)
	case logical.DeleteOperation:
		l.Trace("delete operation")
		err = SyncDelete(ctx, scs, j)
	default:
		l.Trace("operation not defined")
		err = errors.New("operation not defined")
	}
	if err != nil {
		return handleSyncError(ctx, err, j, startTime)
	}
	return handleSyncSuccess(ctx, j, startTime)
}

func buildSyncJobs(evt event.VaultEvent) ([]SyncJob, []string, []driver.DriverName) {
	l := log.WithFields(log.Fields{
		"action":  "buildSyncJobs",
		"eventId": evt.ID,
		"op":      evt.Operation,
		"path":    evt.Path,
		"tenant":  evt.Address,
	})
	l.Trace("start")
	defer l.Trace("end")
	var jobHolder []SyncJob
	var affectedConfigs []string
	var destinationStores []driver.DriverName
	if evt.SyncName != "" && evt.Manual {
		sc, err := backend.GetSyncConfigByName(evt.SyncName)
		if err != nil {
			l.WithError(err).Error("failed to get sync config")
			return jobHolder, affectedConfigs, destinationStores
		}
		jobHolder = append(jobHolder, SyncJob{VaultEvent: evt, SyncConfig: sc})
		affectedConfigs = append(affectedConfigs, sc.Name)
		destinationStores = append(destinationStores, DestinationStoreNames(sc)...)
		return jobHolder, affectedConfigs, destinationStores
	}
	cfgs := backend.TenantNamespaceConfigs(evt)
	l.WithFields(log.Fields{"configs": cfgs}).Trace("configs")
	for _, sc := range cfgs {
		l.WithFields(log.Fields{"config": sc.Name}).Trace("config")
		if NeedsSync(sc, evt) {
			l.WithFields(log.Fields{"config": sc.Name}).Trace("needs sync")
			affectedConfigs = append(affectedConfigs, sc.Name)
			destinationStores = append(destinationStores, DestinationStoreNames(sc)...)
			l.WithFields(log.Fields{"destinationStores": destinationStores}).Trace("destination stores")
			jobHolder = append(jobHolder, SyncJob{VaultEvent: evt, SyncConfig: sc})
		}
	}

	l.WithFields(log.Fields{"jobHolder": jobHolder, "affectedConfigs": affectedConfigs, "destinationStores": destinationStores}).Trace("end")

	return jobHolder, affectedConfigs, destinationStores
}
