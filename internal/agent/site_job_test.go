package agent

import (
	"testing"

	"backupper/internal/model"
)

func TestJobDirSite(t *testing.T) {
	a := newTestAgent(t, "http://127.0.0.1:1")
	if got := a.jobDir(model.Job{Name: "Магазин"}); got != "test_server/magazin" {
		t.Errorf("server job dir = %q", got)
	}
	// A hosting site lives outside the relaying server's folder.
	if got := a.jobDir(model.Job{Name: "shop.ru", Kind: model.JobSite}); got != "sites/shop.ru" {
		t.Errorf("site job dir = %q", got)
	}
}

func TestValidateSiteJob(t *testing.T) {
	site := model.Source{Type: model.SourceSite, Name: "site", Site: &model.SiteSource{Host: "ftp.shop.ru"}}
	ok := model.Job{Name: "shop.ru", Kind: model.JobSite, Sources: []model.Source{site}, Storages: []string{"dav"}}
	if err := validateJob(&ok, nil); err != nil {
		t.Fatalf("valid site job refused: %v", err)
	}
	for name, j := range map[string]model.Job{
		"two sources": {Name: "a", Kind: model.JobSite, Sources: []model.Source{site, site}, Storages: []string{"dav"}},
		"not a site":  {Name: "b", Kind: model.JobSite, Sources: []model.Source{{Type: model.SourceFiles}}, Storages: []string{"dav"}},
		"bad kind":    {Name: "c", Kind: "nope", Sources: []model.Source{site}, Storages: []string{"dav"}},
	} {
		if err := validateJob(&j, nil); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
