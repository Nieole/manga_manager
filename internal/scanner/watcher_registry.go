package scanner

import (
	"log/slog"
	"os"
)

type registrationChange struct{ remove, add bool }

// queueRegistration never waits for a source command. Windows may block that command
// until its reader can deliver another event to this same consumer.
func (fw *FileWatcher) queueRegistration(path string, add bool) {
	fw.registryMu.Lock()
	change := fw.registryPending[path]
	if add {
		change.add = true
	} else {
		change.remove = true
		change.add = false
	}
	fw.registryPending[path] = change
	fw.registryMu.Unlock()
	select {
	case fw.registryWake <- struct{}{}:
	default:
	}
}

func (fw *FileWatcher) startRegistry() {
	fw.registryDone.Add(1)
	go func() {
		defer fw.registryDone.Done()
		for {
			select {
			case <-fw.stopCh:
				return
			case <-fw.registryWake:
			}
			fw.registryMu.Lock()
			pending := fw.registryPending
			fw.registryPending = make(map[string]registrationChange)
			fw.registryMu.Unlock()
			for path, change := range pending {
				select {
				case <-fw.stopCh:
					return
				default:
				}
				fw.registrationOps.Lock()
				if change.remove {
					_ = fw.watcher.Remove(path)
				}
				fw.mu.Lock()
				active := false
				if !fw.stopping {
					for root := range fw.libs {
						if pathUnderRoot(path, root) {
							active = true
							break
						}
					}
				}
				fw.mu.Unlock()
				info, err := os.Stat(path)
				if active && (change.add || change.remove) && err == nil && info.IsDir() {
					if report := fw.watchRecursive(path); report.FirstErr != nil {
						slog.Warn("Failed to watch part of the new subdirectory", "path", path, "watched", report.Watched, "failed", report.Failed, "error", report.FirstErr)
					}
				}
				fw.registrationOps.Unlock()
			}
		}
	}()
}
