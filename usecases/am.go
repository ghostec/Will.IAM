package usecases

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/ghostec/Will.IAM/constants"
	"github.com/ghostec/Will.IAM/models"
	"github.com/ghostec/Will.IAM/repositories"
	extensionsHttp "github.com/topfreegames/extensions/http"
)

// AM define entrypoints for Access Management actions
type AM interface {
	List(prefix string) ([]models.AM, error)
	WithContext(context.Context) AM
}

type am struct {
	repo *repositories.All
	ctx  context.Context
	http *http.Client
	rsUC Roles
}

func (a am) WithContext(ctx context.Context) AM {
	return &am{
		a.repo.WithContext(ctx),
		ctx,
		a.http,
		a.rsUC.WithContext(ctx),
	}
}

func (a am) List(prefix string) ([]models.AM, error) {
	if !strings.Contains(prefix, "::") {
		services, err := a.listServices(prefix)
		if err != nil {
			return nil, err
		}
		ams := make([]models.AM, len(services))
		for i := range services {
			ams[i] = models.AM{
				Prefix:   services[i],
				Complete: false,
			}
		}
		return ams, nil
	}
	parts := strings.Split(prefix, "::")
	service := parts[0]
	if service == constants.AppInfo.Name {
		return a.listWillIAMPermissions(prefix)
	}
	return a.listServicePermissions(service, prefix)
}

func (a am) listServices(prefix string) ([]string, error) {
	services, err := a.repo.Services.List()
	if err != nil {
		return nil, err
	}
	svcs := []string{constants.AppInfo.Name}
	for i := range services {
		svcs = append(svcs, services[i].PermissionName)
	}
	filtered := []string{}
	for i := range svcs {
		if strings.HasPrefix(svcs[i], prefix) {
			filtered = append(filtered, svcs[i])
		}
	}
	return filtered, nil
}

func (a am) listWillIAMPermissions(prefix string) ([]models.AM, error) {
	parts := strings.Split(prefix, "::")
	if len(parts) == 2 {
		actions, err := a.listWillIAMActions(parts[1])
		if err != nil {
			return nil, err
		}
		ams := make([]models.AM, len(actions))
		for i := range actions {
			ams[i] = models.AM{
				Prefix:   fmt.Sprintf("%s::%s", parts[0], actions[i]),
				Complete: false,
			}
		}
		return ams, nil
	}
	ams, err := a.listWillIAMResourceHierarchies(parts[1], parts[2])
	if err != nil {
		return nil, err
	}
	for i := range ams {
		ams[i].Prefix = fmt.Sprintf("%s::%s::%s", parts[0], parts[1], ams[i].Prefix)
	}
	return append(
		[]models.AM{models.AM{
			Prefix: fmt.Sprintf("%s::%s::*", parts[0], parts[1]), Complete: true,
		}}, ams...,
	), nil
}

func (a am) listWillIAMActions(prefix string) ([]string, error) {
	all := append(constants.RolesActions, constants.ServiceAccountsActions...)
	all = append(all, constants.ServicesActions...)
	keep := []string{}
	for i := range all {
		if ok := strings.HasPrefix(all[i], prefix); ok {
			keep = append(keep, all[i])
		}
	}
	return keep, nil
}

func actionsContains(actions []string, action string) bool {
	for _, aa := range actions {
		if aa == action {
			return true
		}
	}
	return false
}

func (a am) listWillIAMResourceHierarchies(
	action, prefix string,
) ([]models.AM, error) {
	if actionsContains(constants.RolesActions, action) {
		return a.listRolesActionsRH(action, prefix)
	}
	if actionsContains(constants.ServiceAccountsActions, action) {
		return []models.AM{}, nil
	}
	if actionsContains(constants.ServicesActions, action) {
		return []models.AM{}, nil
	}
	return []models.AM{}, nil
}

func (a am) listRolesActionsRH(
	action, prefix string,
) ([]models.AM, error) {
	if action == "CreateRoles" || action == "ListRoles" {
		return []models.AM{}, nil
	}
	rs, err := a.rsUC.WithNamePrefix(prefix, 10)
	if err != nil {
		return nil, err
	}
	ams := make([]models.AM, len(rs))
	for i := range rs {
		ams[i] = models.AM{
			Prefix:   rs[i].ID,
			Alias:    rs[i].Name,
			Complete: true,
		}
	}
	return ams, nil
}

func (a am) listServicePermissions(
	service, prefix string,
) ([]models.AM, error) {
	svc, err := a.repo.Services.WithPermissionName(service)
	if err != nil {
		return nil, err
	}
	prefixWOSvc := strings.Join(strings.Split(prefix, "::")[1:], "::")
	req, err := http.NewRequest(
		"GET", fmt.Sprintf("%s?prefix=%s", svc.AMURL, prefixWOSvc), nil,
	)
	if err != nil {
		return nil, err
	}
	res, err := a.http.Do(req.WithContext(a.ctx))
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	var ams []models.AM
	err = json.Unmarshal(body, &ams)
	if err != nil {
		return nil, err
	}
	for i := range ams {
		ams[i].Prefix = fmt.Sprintf("%s::%s", service, ams[i].Prefix)
	}
	return ams, nil
}

// NewAM ctor
func NewAM(repo *repositories.All, rsUC Roles) AM {
	return &am{repo: repo, http: extensionsHttp.New(), rsUC: rsUC}
}
