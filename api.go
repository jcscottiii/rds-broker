package main

import (
	"github.com/go-martini/martini"
	"github.com/jinzhu/gorm"
	"github.com/martini-contrib/render"

	"crypto/aes"
	"encoding/json"
	"io/ioutil"
	"net/http"
)

// CreateInstance
// URL: /v2/service_instances/:id
// Request:
// {
//   "service_id":        "service-guid-here",
//   "plan_id":           "plan-guid-here",
//   "organization_guid": "org-guid-here",
//   "space_guid":        "space-guid-here"
// }
func CreateInstance(p martini.Params, req *http.Request, r render.Render, db *gorm.DB, s *Settings) {
	instance := Instance{}

	db.Where("uuid = ?", p["id"]).First(&instance)

	if instance.Id > 0 {
		r.JSON(http.StatusConflict, Response{"The instance already exists"})
		return
	}

	var sr serviceReq
	var plan *Plan

	if req.Body == nil {
		r.JSON(http.StatusBadRequest, Response{"No request"})
		return
	}

	body, _ := ioutil.ReadAll(req.Body)

	json.Unmarshal(body, &sr)
	instance.PlanId = sr.PlainId
	instance.OrgGuid = sr.OrganizationGuid
	instance.SpaceGuid = sr.SpaceGuid

	plan = FindPlan(instance.PlanId)

	if plan == nil {
		r.JSON(http.StatusBadRequest, Response{"The plan requested does not exist"})
		return
	}

	instance.Uuid = p["id"]

	instance.Adapter = plan.Adapter

	instance.Database = "db" + randStr(15)
	instance.Username = "u" + randStr(15)
	instance.Salt = GenerateSalt(aes.BlockSize)
	password := randStr(25)
	if err := instance.SetPassword(password, s.EncryptionKey); err != nil {
		desc := "There was an error setting the password" + err.Error()
		r.JSON(http.StatusInternalServerError, Response{desc})
		return
	}

	instance.DbType = plan.DbType

	// Create the database adapter.
	adapter, err := s.DbAdapterFactory.CreateDBAdapter(plan, db)
	if err != nil {
		desc := "There was an error creating the instance. Error: " + err.Error()
		r.JSON(http.StatusInternalServerError, Response{desc})
		return
	}
	var status DBInstanceState
	// Create the database instance.
	if status, err = (*adapter).CreateDB(&instance, password); status == InstanceNotCreated {
		desc := "There was an error creating the instance."
		if err != nil {
			desc = desc + " Error: " + err.Error()
		}
		r.JSON(http.StatusInternalServerError, Response{desc})
		return
	}

	switch status {
	case InstanceInProgress:
		// Instance creation in progress
	case InstanceReady:
		// Instance ready
	}

	instance.State = status
	db.Save(&instance)

	r.JSON(http.StatusCreated, Response{"The instance was created"})
}

// BindInstance
// URL: /v2/service_instances/:instance_id/service_bindings/:binding_id
// Request:
// {
//   "plan_id":        "plan-guid-here",
//   "service_id":     "service-guid-here",
//   "app_guid":       "app-guid-here"
// }
func BindInstance(p martini.Params, r render.Render, db *gorm.DB, s *Settings) {
	instance := Instance{}

	db.Where("uuid = ?", p["instance_id"]).First(&instance)
	if instance.Id == 0 {
		r.JSON(404, Response{"Instance not found"})
		return
	}
	password, err := instance.GetPassword(s.EncryptionKey)
	if err != nil {
		r.JSON(http.StatusInternalServerError, "Unable to get instance password.")
	}

	plan := FindPlan(instance.PlanId)

	if plan == nil {
		r.JSON(http.StatusBadRequest, Response{"The plan requested does not exist"})
		return
	}

	// Create the database adapter.
	adapter, err := s.DbAdapterFactory.CreateDBAdapter(plan, db)
	if err != nil {
		desc := "There was an error creating the instance. Error: " + err.Error()
		r.JSON(http.StatusInternalServerError, Response{desc})
		return
	}

	var credentials map[string]string
	// Bind the database instance to the application.
	originalInstanceState := instance.State
	if credentials, err = (*adapter).BindDBToApp(&instance, password); err != nil {
		desc := "There was an error binding the database instance to the application."
		if err != nil {
			desc = desc + " Error: " + err.Error()
		}
		r.JSON(http.StatusInternalServerError, Response{desc})
		return
	}

	// If the state of the instance has changed, update it.
	if instance.State != originalInstanceState {
		db.Save(&instance)
	}

	response := map[string]interface{}{
		"credentials": credentials,
	}
	r.JSON(http.StatusCreated, response)
}

// DeleteInstance
// URL: /v2/service_instances/:id
// Request:
// {
//   "service_id": "service-id-here"
//   "plan_id":    "plan-id-here"
// }
func DeleteInstance(p martini.Params, r render.Render, db *gorm.DB, s *Settings) {
	instance := Instance{}

	db.Where("uuid = ?", p["id"]).First(&instance)

	if instance.Id == 0 {
		r.JSON(http.StatusNotFound, Response{"Instance not found"})
		return
	}

	var plan *Plan
	plan = FindPlan(instance.PlanId)

	if plan == nil {
		r.JSON(http.StatusBadRequest, Response{"The plan requested does not exist"})
		return
	}
	// Create the database adapter.
	adapter, err := s.DbAdapterFactory.CreateDBAdapter(plan, db)
	if err != nil {
		desc := "There was an error deleting the instance. Error: " + err.Error()
		r.JSON(http.StatusInternalServerError, Response{desc})
		return
	}
	var status DBInstanceState
	// Delete the database instance.
	if status, err = (*adapter).DeleteDB(&instance); status == InstanceNotGone {
		desc := "There was an error deleting the instance."
		if err != nil {
			desc = desc + " Error: " + err.Error()
		}
		r.JSON(http.StatusInternalServerError, Response{desc})
		return
	}
	db.Delete(&instance)
	r.JSON(http.StatusOK, Response{"The instance was deleted"})
}
