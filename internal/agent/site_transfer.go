package agent

import (
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"

	"backupper/internal/model"
	"backupper/internal/storage"
)

type siteTransfer struct {
	Name     string                               `json:"name"`
	Runs     []*model.Run                         `json:"runs"`
	Telegram map[string]storage.TelegramSiteIndex `json:"telegram"`
}

func (a *Agent) hPauseSite(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a.mu.Lock()
	if a.active != nil && a.active.JobID == id {
		a.mu.Unlock()
		httpErr(w, 409, errors.New("дождитесь завершения запуска сайта"))
		return
	}
	for _, run := range a.queue {
		if run.JobID == id {
			a.mu.Unlock()
			httpErr(w, 409, errors.New("сайт находится в очереди"))
			return
		}
	}
	jobBefore, ok := a.state.Job(id)
	if ok && a.state.cleanup != nil {
		for _, source := range jobBefore.Sources {
			if source.Site != nil && a.state.cleanup.HasPending(*source.Site) {
				a.mu.Unlock()
				httpErr(w, 409, errors.New("дождитесь удаления временных файлов сайта на хостинге"))
				return
			}
		}
	}
	var job model.Job
	err := a.state.update(func(c *Config) error {
		for i := range c.Jobs {
			if c.Jobs[i].ID == id && c.Jobs[i].Kind == model.JobSite {
				c.Jobs[i].Migrating = true
				job = c.Jobs[i]
				return nil
			}
		}
		return errors.New("сайт не найден")
	})
	a.mu.Unlock()
	if err != nil {
		httpErr(w, 409, err)
		return
	}
	a.Reschedule()
	writeJSONResp(w, job)
}

func (a *Agent) hExportSite(w http.ResponseWriter, r *http.Request) {
	id, name := r.URL.Query().Get("job"), strings.TrimSpace(r.URL.Query().Get("name"))
	if id == "" || name == "" {
		httpErr(w, 400, errors.New("не указаны сайт и задача"))
		return
	}
	for _, run := range a.Active() {
		if run.JobID == id {
			httpErr(w, 409, errors.New("дождитесь завершения запуска сайта"))
			return
		}
	}
	runs := a.state.Runs(id, 0, true)
	job, exists := a.state.Job(id)
	if exists && (job.Kind != model.JobSite || job.Name != name || !job.Migrating) {
		httpErr(w, 409, errors.New("выключите расписание исходного сайта и проверьте его название"))
		return
	}
	if !exists && len(runs) == 0 {
		httpErr(w, 404, errors.New("история исходного сайта не найдена"))
		return
	}
	for _, run := range runs {
		if run.JobName != name {
			httpErr(w, 409, errors.New("название сайта в истории отличается; требуется проверка вручную"))
			return
		}
	}
	out := siteTransfer{Name: name, Runs: runs, Telegram: map[string]storage.TelegramSiteIndex{}}
	for _, sc := range a.state.Config().Storages {
		if sc.Kind != model.StorageTelegram {
			continue
		}
		dir := storage.Clean(path.Join(sc.Root, model.SitesFolder, Slug(name)))
		index, err := storage.ExportTelegramSite(sc, dir)
		if err != nil {
			httpErr(w, 500, err)
			return
		}
		out.Telegram[sc.ID] = index
	}
	writeJSONResp(w, out)
}

func (a *Agent) hImportSite(w http.ResponseWriter, r *http.Request) {
	job, ok := a.state.Job(r.PathValue("id"))
	if !ok || job.Kind != model.JobSite {
		httpErr(w, 404, errors.New("сайт не найден"))
		return
	}
	if !job.Migrating {
		httpErr(w, 409, errors.New("выключите расписание сайта перед импортом"))
		return
	}
	for _, run := range a.Active() {
		if run.JobID == job.ID {
			httpErr(w, 409, errors.New("дождитесь завершения запуска сайта"))
			return
		}
	}
	var in siteTransfer
	if err := decode(r, &in); err != nil {
		httpErr(w, 400, err)
		return
	}
	if in.Name != job.Name {
		httpErr(w, 409, errors.New("названия сайтов не совпадают"))
		return
	}
	for id, index := range in.Telegram {
		sc, found := a.state.Storage(id)
		if !found || sc.Kind != model.StorageTelegram {
			httpErr(w, 409, fmt.Errorf("хранилище Telegram %s не найдено", id))
			return
		}
		dir := storage.Clean(path.Join(sc.Root, model.SitesFolder, Slug(job.Name)))
		if err := storage.ImportTelegramSite(sc, dir, index); err != nil {
			httpErr(w, 409, err)
			return
		}
	}
	if err := a.state.ImportSiteRuns(job.ID, in.Runs); err != nil {
		httpErr(w, 409, err)
		return
	}
	if job.MigrationSource != "" {
		if err := a.state.update(func(c *Config) error {
			for i := range c.Jobs {
				if c.Jobs[i].ID == job.ID {
					c.Jobs[i].MigrationImported = true
					return nil
				}
			}
			return errors.New("сайт не найден")
		}); err != nil {
			httpErr(w, 500, err)
			return
		}
	}
	writeJSONResp(w, map[string]bool{"ok": true})
}
