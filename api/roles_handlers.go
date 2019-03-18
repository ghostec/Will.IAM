package api

import (
	"encoding/json"
	"io/ioutil"
	"net/http"

	"github.com/ghostec/Will.IAM/errors"
	"github.com/ghostec/Will.IAM/models"
	"github.com/ghostec/Will.IAM/usecases"
	"github.com/gorilla/mux"
	"github.com/topfreegames/extensions/middleware"
)

func rolesCreatePermissionHandler(
	sasUC usecases.ServiceAccounts, rsUC usecases.Roles,
) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		l := middleware.GetLogger(r.Context())
		qs := r.URL.Query()
		permissionSl := qs["permission"]
		if len(permissionSl) == 0 {
			Write(w, http.StatusUnprocessableEntity, `{"error": "querystrings.permission is required"}`)
			return
		}
		sameP, err := models.BuildPermission(permissionSl[0])
		if err != nil {
			Write(w, http.StatusBadRequest, `{"error": "querystrings.permission malformed"}`)
			return
		}
		sameP.OwnershipLevel = models.OwnershipLevels.Owner

		saID, _ := getServiceAccountID(r.Context())
		has, err := sasUC.WithContext(r.Context()).
			HasPermissionString(saID, sameP.String())
		if err != nil {
			l.Error(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if !has {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		rID := mux.Vars(r)["id"]
		p, err := models.BuildPermission(permissionSl[0])
		if err != nil {
			Write(w, http.StatusBadRequest, `{"error": "querystrings.permission malformed"}`)
			return
		}
		err = rsUC.WithContext(r.Context()).CreatePermission(rID, &p)
		if err != nil {
			l.Error(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}
}

func rolesCreateHandler(
	sasUC usecases.ServiceAccounts, rsUC usecases.Roles,
) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		l := middleware.GetLogger(r.Context())
		rwn, err := processRoleWithNestedFromReq(r, sasUC)
		if err != nil {
			statusCode := http.StatusInternalServerError
			if _, ok := err.(errors.ErrorWithStatusCode); ok {
				statusCode = err.(errors.ErrorWithStatusCode).
					StatusCode()
			}
			l.WithError(err).Error("rolesUpdateHandler processRoleWithNestedFromReq")
			w.WriteHeader(statusCode)
			return
		}
		v := rwn.Validate()
		if !v.Valid() {
			WriteBytes(w, http.StatusUnprocessableEntity, v.Errors())
			return
		}
		err = rsUC.WithContext(r.Context()).Create(rwn)
		if err != nil {
			l.Error(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}
}

func rolesUpdateHandler(
	sasUC usecases.ServiceAccounts, rsUC usecases.Roles,
) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		l := middleware.GetLogger(r.Context())
		rwn, err := processRoleWithNestedFromReq(r, sasUC)
		if err != nil {
			statusCode := http.StatusInternalServerError
			if _, ok := err.(*errors.UserDoesntHaveAllPermissionsError); ok {
				statusCode = err.(*errors.UserDoesntHaveAllPermissionsError).
					StatusCode()
			}
			l.WithError(err).Error("rolesUpdateHandler processRoleWithNestedFromReq")
			w.WriteHeader(statusCode)
			return
		}
		v := rwn.Validate()
		if !v.Valid() {
			WriteBytes(w, http.StatusUnprocessableEntity, v.Errors())
			return
		}
		rwn.ID = mux.Vars(r)["id"]
		if err = rsUC.WithContext(r.Context()).Update(rwn); err != nil {
			l.WithError(err).Error("rolesUpdateHandler rsUC.Update")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		// TODO: audit
		w.WriteHeader(http.StatusOK)
	}
}

func processRoleWithNestedFromReq(
	r *http.Request, sasUC usecases.ServiceAccounts,
) (*usecases.RoleWithNested, error) {
	body, err := ioutil.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		return nil, err
	}
	rwn := &usecases.RoleWithNested{}
	err = json.Unmarshal(body, rwn)
	if err != nil {
		return nil, err
	}
	saID, _ := getServiceAccountID(r.Context())
	rwn.Permissions, err = models.BuildPermissions(rwn.PermissionsStrings)
	if err != nil {
		return nil, err
	}
	for i := range rwn.PermissionsStrings {
		if alias, ok := rwn.PermissionsAliases[rwn.PermissionsStrings[i]]; ok {
			rwn.Permissions[i].Alias = alias
		}
	}
	has, err := sasUC.WithContext(r.Context()).
		HasAllOwnerPermissions(saID, rwn.Permissions)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, errors.NewUserDoesntHaveAllPermissionsError()
	}
	return rwn, nil
}

func rolesListHandler(
	rsUC usecases.Roles,
) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		l := middleware.GetLogger(r.Context())
		rsSl, err := rsUC.WithContext(r.Context()).List()
		if err != nil {
			l.Error(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		bts, err := keepJSONFieldsBytes(rsSl, "id", "name")
		if err != nil {
			l.Error(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		WriteBytes(w, 200, bts)
	}
}

func rolesGetHandler(
	rsUC usecases.Roles,
) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		l := middleware.GetLogger(r.Context())
		id := mux.Vars(r)["id"]
		rsUCc := rsUC.WithContext(r.Context())
		role, err := rsUCc.Get(id)
		if err != nil {
			l.WithError(err).Error("rolesViewHandler rsUC.Get")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		bts, err := json.Marshal(role)
		if err != nil {
			l.Error(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		WriteBytes(w, 200, bts)
	}
}
