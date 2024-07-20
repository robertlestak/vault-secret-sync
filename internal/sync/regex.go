package sync

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/robertlestak/vault-secret-sync/internal/transforms"
	log "github.com/sirupsen/logrus"
)

type manualSyncTask struct {
	dest        SyncClient
	srcPath     string
	rewritePath string
}

func manualRegexSyncWorker(ctx context.Context, j SyncJob, taskCh chan manualSyncTask, errCh chan error) {
	for task := range taskCh {
		if shouldFilterSecret(j, j.SyncConfig.Spec.Source.GetPath(), task.dest.GetPath()) {
			errCh <- nil
			continue
		}
		if shouldDryRun(j, task.dest, j.SyncConfig.Spec.Source.GetPath(), task.rewritePath) {
			errCh <- nil
			continue
		}
		if err := CreateOne(ctx, j, j.SyncConfig.Spec.Source, task.dest, task.srcPath, task.rewritePath); err != nil {
			log.WithError(err).Error("sync job failed")
			errCh <- err
		} else {
			errCh <- nil
		}
	}
}

func handleManualRegexSync(ctx context.Context, sc *SyncClients, j SyncJob) error {
	l := log.WithFields(log.Fields{"action": "handleManualRegexSync"})
	l.Debug("manual regex sync")

	// Check if sc or sc.Source is nil
	if sc == nil || sc.Source == nil {
		l.Error("SyncClients or Source is nil")
		return fmt.Errorf("SyncClients or Source is nil")
	}

	// Check if sc.Source.GetPath() returns nil
	sourcePath := sc.Source.GetPath()
	if sourcePath == "" {
		l.Error("Source path is empty")
		return fmt.Errorf("Source path is empty")
	}

	highestNonRegexPath := findHighestNonRegexPath(sourcePath)
	list, err := LoopWildcardRecursive(ctx, sc.Source, highestNonRegexPath)
	if err != nil {
		l.Error(err)
		return err
	}

	// Check if list is nil
	if list == nil {
		l.Error("List returned by LoopWildcardRecursive is nil")
		return fmt.Errorf("List returned by LoopWildcardRecursive is nil")
	}

	l.WithFields(log.Fields{"list": list}).Debug("found list")

	strictRegexPattern := "^" + sourcePath + "$"
	rx, err := regexp.Compile(strictRegexPattern)
	if err != nil {
		return err
	}
	l.WithFields(log.Fields{"regex": strictRegexPattern}).Debug("compiled regex")
	l.WithFields(log.Fields{"destStores": len(sc.Dest)}).Debug("syncing to dest stores")

	taskCh := make(chan manualSyncTask, len(sc.Dest)*len(list))
	errCh := make(chan error, len(sc.Dest)*len(list))

	// Number of worker goroutines
	const numWorkers = 10 // Adjust this number based on your requirements

	// Start worker goroutines
	for i := 0; i < numWorkers; i++ {
		go manualRegexSyncWorker(ctx, j, taskCh, errCh)
	}

	// Create tasks and send them to the task channel
	for _, d := range sc.Dest {
		ll := log.WithFields(log.Fields{"store": d.Driver()})
		ll.Debug("dest store")
		for _, p := range list {
			if !rx.MatchString(p) {
				ll.WithField("path", p).Debug("skipping non-matching path")
				continue
			}
			matches := rx.FindStringSubmatch(p)
			rewritePath := d.GetPath()

			if hasCaptureGroups(sourcePath) {
				for i, match := range matches {
					if i == 0 {
						continue
					}
					groupName := fmt.Sprintf("$%d", i)
					rewritePath = strings.ReplaceAll(rewritePath, groupName, match)
				}
			} else {
				rewritePath = path.Join(rewritePath, p[len(highestNonRegexPath):])
			}

			taskCh <- manualSyncTask{dest: d, srcPath: p, rewritePath: rewritePath}
		}
	}
	close(taskCh)

	var errors []error
	for i := 0; i < len(sc.Dest)*len(list); i++ {
		if err := <-errCh; err != nil {
			errors = append(errors, err)
		}
	}
	close(errCh)

	if len(errors) > 0 {
		return fmt.Errorf("errors: %v", errors)
	}

	l.Debug("manual regex sync complete")
	return nil
}

type syncTask struct {
	dest        SyncClient
	srcPath     string
	rewritePath string
}

func regexSyncWorker(ctx context.Context, j SyncJob, taskCh chan syncTask, errCh chan error) {
	for task := range taskCh {
		if shouldFilterSecret(j, j.SyncConfig.Spec.Source.GetPath(), task.dest.GetPath()) {
			errCh <- nil
			continue
		}
		if shouldDryRun(j, task.dest, j.SyncConfig.Spec.Source.GetPath(), task.rewritePath) {
			errCh <- nil
			continue
		}
		if err := CreateOne(ctx, j, j.SyncConfig.Spec.Source, task.dest, task.srcPath, task.rewritePath); err != nil {
			log.WithError(err).Error("sync job failed")
			errCh <- err
		} else {
			errCh <- nil
		}
	}
}

func handleRegexSync(ctx context.Context, sc *SyncClients, j SyncJob) error {
	l := log.WithFields(log.Fields{"action": "handleRegexSync"})
	l.Debug("handling regex sync")

	rx, err := regexp.Compile(sc.Source.GetPath())
	if err != nil {
		l.WithFields(log.Fields{
			"error":      err,
			"sourcePath": sc.Source.GetPath(),
		}).Error("failed to compile regex")
		return err
	}

	dp, mp := transforms.VaultPathsFromPath(j.VaultEvent.Path)
	if !rx.MatchString(j.VaultEvent.Path) && !rx.MatchString(strippedPath(j.VaultEvent.Path)) {
		l.WithFields(log.Fields{
			"regex":    sc.Source.GetPath(),
			"path":     j.VaultEvent.Path,
			"dataPath": dp,
			"metaPath": mp,
		}).Debug("no regex match")
		return nil
	}
	l.Debug("regex match")
	sp := strippedPath(j.VaultEvent.Path)
	matches := rx.FindStringSubmatch(sp)

	taskCh := make(chan syncTask, len(sc.Dest))
	errCh := make(chan error, len(sc.Dest))

	// Number of worker goroutines
	const numWorkers = 10 // Adjust this number based on your requirements

	// Start worker goroutines
	for i := 0; i < numWorkers; i++ {
		go regexSyncWorker(ctx, j, taskCh, errCh)
	}

	// Create tasks and send them to the task channel
	for _, d := range sc.Dest {
		rewritePath := d.GetPath()

		if hasCaptureGroups(sc.Source.GetPath()) {
			for i, match := range matches {
				if i == 0 {
					continue
				}
				groupName := fmt.Sprintf("$%d", i)
				rewritePath = strings.ReplaceAll(rewritePath, groupName, match)
			}
		} else {
			rewritePath = path.Join(rewritePath, sp[len(findHighestNonRegexPath(sc.Source.GetPath())):])
		}

		taskCh <- syncTask{dest: d, srcPath: sp, rewritePath: rewritePath}
	}
	close(taskCh)

	var errors []error
	for i := 0; i < len(sc.Dest); i++ {
		if err := <-errCh; err != nil {
			errors = append(errors, err)
		}
	}
	close(errCh)

	if len(errors) > 0 {
		return fmt.Errorf("errors: %v", errors)
	}

	return nil
}

type manualDeleteTask struct {
	dest        SyncClient
	rewritePath string
}

func manualRegexDeleteWorker(ctx context.Context, j SyncJob, taskCh chan manualDeleteTask, errCh chan error) {
	for task := range taskCh {
		if shouldFilterSecret(j, j.SyncConfig.Spec.Source.GetPath(), task.dest.GetPath()) {
			errCh <- nil
			continue
		}
		if shouldDryRun(j, task.dest, j.SyncConfig.Spec.Source.GetPath(), task.rewritePath) {
			errCh <- nil
			continue
		}
		if err := task.dest.DeleteSecret(ctx, task.rewritePath); err != nil {
			log.WithError(err).Error("delete job failed")
			errCh <- err
		} else {
			errCh <- nil
		}
	}
}

func handleManualRegexDelete(ctx context.Context, sc *SyncClients, j SyncJob) error {
	l := log.WithFields(log.Fields{"action": "handleManualRegexDelete"})
	l.Debug("manual regex delete")

	highestNonRegexPath := findHighestNonRegexPath(sc.Source.GetPath())
	list, err := LoopWildcardRecursive(ctx, sc.Source, highestNonRegexPath)
	if err != nil {
		return err
	}
	l.WithFields(log.Fields{"list": list}).Debug("found list")
	l.WithFields(log.Fields{"regex": sc.Source.GetPath()}).Debug("compiling regex")
	rx, err := regexp.Compile(sc.Source.GetPath())
	if err != nil {
		return err
	}

	taskCh := make(chan manualDeleteTask, len(sc.Dest)*len(list))
	errCh := make(chan error, len(sc.Dest)*len(list))

	// Number of worker goroutines
	const numWorkers = 10 // Adjust this number based on your requirements

	// Start worker goroutines
	for i := 0; i < numWorkers; i++ {
		go manualRegexDeleteWorker(ctx, j, taskCh, errCh)
	}

	// Create tasks and send them to the task channel
	for _, d := range sc.Dest {
		for _, p := range list {
			if !rx.MatchString(p) {
				continue
			}
			matches := rx.FindStringSubmatch(p)
			if len(matches) > 0 {
				rewritePath := d.GetPath()
				for i, match := range matches {
					groupName := fmt.Sprintf("$%d", i)
					rewritePath = strings.ReplaceAll(rewritePath, groupName, match)
				}
				taskCh <- manualDeleteTask{dest: d, rewritePath: rewritePath}
			}
		}
	}
	close(taskCh)

	var errors []error
	for i := 0; i < len(sc.Dest)*len(list); i++ {
		if err := <-errCh; err != nil {
			errors = append(errors, err)
		}
	}
	close(errCh)

	if len(errors) > 0 {
		return fmt.Errorf("errors: %v", errors)
	}

	return nil
}

type deleteTask struct {
	dest        SyncClient
	rewritePath string
}

func regexDeleteWorker(ctx context.Context, j SyncJob, taskCh chan deleteTask, errCh chan error) {
	for task := range taskCh {
		if shouldFilterSecret(j, j.SyncConfig.Spec.Source.GetPath(), task.dest.GetPath()) {
			errCh <- nil
			continue
		}
		if shouldDryRun(j, task.dest, j.SyncConfig.Spec.Source.GetPath(), task.rewritePath) {
			errCh <- nil
			continue
		}
		if err := task.dest.DeleteSecret(ctx, task.rewritePath); err != nil {
			log.WithError(err).Error("delete job failed")
			errCh <- err
		} else {
			errCh <- nil
		}
	}
}

func handleRegexDelete(ctx context.Context, sc *SyncClients, j SyncJob) error {
	l := log.WithFields(log.Fields{"action": "handleRegexDelete"})
	l.Trace("handling regex delete")

	rx, err := regexp.Compile(sc.Source.GetPath())
	if err != nil || (!rx.MatchString(j.VaultEvent.Path) && !rx.MatchString(strippedPath(j.VaultEvent.Path))) {
		l.WithFields(log.Fields{
			"regex": sc.Source.GetPath(),
			"path":  j.VaultEvent.Path,
		}).Debug("no regex match")
		return nil
	}
	l.Debug("regex match")
	sp := strippedPath(j.VaultEvent.Path)
	matches := rx.FindStringSubmatch(sp)
	l.WithFields(log.Fields{"matches": matches}).Debug("found matches")

	taskCh := make(chan deleteTask, len(sc.Dest))
	errCh := make(chan error, len(sc.Dest))

	// Number of worker goroutines
	const numWorkers = 10 // Adjust this number based on your requirements

	// Start worker goroutines
	for i := 0; i < numWorkers; i++ {
		go regexDeleteWorker(ctx, j, taskCh, errCh)
	}

	// Create tasks and send them to the task channel
	for _, d := range sc.Dest {
		rewritePath := d.GetPath()

		if hasCaptureGroups(sc.Source.GetPath()) {
			for i, match := range matches {
				if i == 0 {
					continue
				}
				groupName := fmt.Sprintf("$%d", i)
				rewritePath = strings.ReplaceAll(rewritePath, groupName, match)
			}
		} else {
			rewritePath = path.Join(rewritePath, sp[len(findHighestNonRegexPath(sc.Source.GetPath())):])
		}

		taskCh <- deleteTask{dest: d, rewritePath: rewritePath}
	}
	close(taskCh)

	var errors []error
	for i := 0; i < len(sc.Dest); i++ {
		if err := <-errCh; err != nil {
			errors = append(errors, err)
		}
	}
	close(errCh)

	if len(errors) > 0 {
		return fmt.Errorf("errors: %v", errors)
	}

	return nil
}
