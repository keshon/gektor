package media

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/keshon/gektor/internal/process"
)

func (s *Service) command(ctx context.Context, app string, args []string, dir string, limit int64) (process.Result, error) {
	status, err := s.Apps.Require(ctx, app)
	if err != nil {
		return process.Result{}, err
	}

	run := s.run
	if run == nil {
		run = process.Run
	}

	child, cancel := context.WithCancel(ctx)
	defer cancel()

	done := make(chan struct{})
	observed := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-done:
				observed <- nil
				return
			case <-child.Done():
				observed <- nil
				return
			case <-ticker.C:
				var total int64
				err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
					if err != nil {
						if os.IsNotExist(err) {
							return nil
						}

						return err
					}

					if d.Type()&os.ModeSymlink != 0 {
						return fmt.Errorf("unexpected link in media work directory")
					}

					if !d.IsDir() {
						info, err := d.Info()
						if err != nil {
							if os.IsNotExist(err) {
								return nil
							}

							return err
						}

						total += info.Size()
					}

					return nil
				})
				if err != nil || total > limit {
					cancel()
					if err == nil {
						err = fmt.Errorf("media temporary data exceeded %d bytes", limit)
					}

					observed <- err

					return
				}
			}
		}
	}()
	result, runErr := run(child, process.Request{
		Program:   status.Path,
		Args:      args,
		Directory: dir,
		Timeout:   10 * time.Minute,
	})
	close(done)
	if err := <-observed; err != nil {
		return result, err
	}

	if runErr != nil {
		return result, fmt.Errorf("%s: %w; %s", app, runErr, brief(result.Stderr, 2048))
	}

	return result, nil
}

func brief(raw []byte, n int) string {
	if len(raw) > n {
		raw = raw[:n]
	}

	return strings.TrimSpace(string(raw))
}
