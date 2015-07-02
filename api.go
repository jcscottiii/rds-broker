package main

import (
	"github.com/go-martini/martini"
	"github.com/jinzhu/gorm"
	"github.com/martini-contrib/render"

	"crypto/aes"
	"encoding/json"
	"fmt"
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

	// Create the database instance
	status, err := s.DbAdapterFactory.CreateDB(plan, &instance, db, password)
	if err != nil {
		desc := "There was an error creating the instance. Error: " + err.Error()
		r.JSON(http.StatusInternalServerError, Response{desc})
		return
	}
	// Double check in case the developer forgets to send an error back.
	if status == InstanceNotCreated {
		desc := "There was an error creating the instance."
		r.JSON(http.StatusInternalServerError, Response{desc})
		return
	}

	switch status {
	case InstanceInProgress:
		// Instance creation in progress
	case InstanceReady:
		// Instance ready
	}

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

	if instance.Adapter == "shared" {
		password, err := instance.GetPassword(s.EncryptionKey)
		if err != nil {
			r.JSON(http.StatusInternalServerError, "")
		}

		uri := fmt.Sprintf("postgres://%s:%s@%s:%s/%s",
			instance.Username,
			password,
			s.DbConfig.Url,
			s.DbConfig.Port,
			instance.Database)

		credentials := map[string]string{
			"uri":      uri,
			"username": instance.Username,
			"password": password,
			"host":     s.DbConfig.Url,
			"db_name":  instance.Database,
		}

		response := map[string]interface{}{
			"credentials": credentials,
		}
		r.JSON(http.StatusCreated, response)
	} else if instance.Adapter == "dedicated" {
		r.JSON(http.StatusNotImplemented, Response{"Dedicated instance support not implemented yet."})
	} else {
		r.JSON(http.StatusInternalServerError, Response{"Unsupported adapter type: " + instance.Adapter + ". Unable to bind."})
	}
}

// DeleteInstance
// URL: /v2/service_instances/:id
// Request:
// {
//   "service_id": "service-id-here"
//   "plan_id":    "plan-id-here"
// }
func DeleteInstance(p martini.Params, r render.Render, db *gorm.DB) {
	instance := Instance{}

	db.Where("uuid = ?", p["id"]).First(&instance)

	if instance.Id == 0 {
		r.JSON(http.StatusNotFound, Response{"Instance not found"})
		return
	}

	if instance.Adapter == "shared" {
		db.Exec(fmt.Sprintf("DROP DATABASE %s;", instance.Database))
		db.Exec(fmt.Sprintf("DROP USER %s;", instance.Username))

		db.Delete(&instance)

		r.JSON(http.StatusOK, Response{"The instance was deleted"})
	} else if instance.Adapter == "dedicated" {
		r.JSON(http.StatusNotImplemented, Response{"Dedicated instance support not implemented yet."})
	} else {
		r.JSON(http.StatusInternalServerError, Response{"Unsupported adapter type: " + instance.Adapter + ". Unable to delete."})
	}
}

/*
 *
 *	RDS Broker specific APIs
 *
 *
 */

// GetDatabases retrieves all database instances.
// URL: /api/databases/
// Request:
// GET
// {
// }
func GetDatabases(p martini.Params, r render.Render, db *gorm.DB) {
	dbConfigs := make([]DBConfig, 0)
	db.Find(&dbConfigs)
	r.JSON(http.StatusOK, dbConfigs)
}

// RegisterDatabase
// URL: /api/databases/
// Request:
// POST
// {
//   "db_type": "your-db-type",
//   "url": "http://hostname.com",
//   "username": "your-username",
//   "password": "your-password",
//   "db_name": "your-database-name",
//   "sslmode": "disabled",
//   "port": "5432",
//   "plan_id": "your-plan-id"
// }
func RegisterDatabase(p martini.Params, req *http.Request, r render.Render, db *gorm.DB) {
	// Read the body of the request.
	body, _ := ioutil.ReadAll(req.Body)

	// Try to unmarshal the json body into a DBConfig struct.
	var dbConfig DBConfig
	if err := json.Unmarshal(body, &dbConfig); err != nil {
		r.JSON(http.StatusBadRequest, Response{"Unable to parse request body"})
		return
	}

	// Check that there is not already a db config registered with the same values.
	var existingDbConfig DBConfig
	db.Where(&dbConfig).First(&existingDbConfig)
	if existingDbConfig.ID > 0 {
		r.JSON(http.StatusConflict, Response{"DB Config already exists"})
		return
	}

	// Insert a new DB Config into the database.
	db.NewRecord(&dbConfig)
	if count := db.Save(&dbConfig).RowsAffected; count == 1 {
		r.JSON(http.StatusCreated, Response{"Entity created. Id " + fmt.Sprint(dbConfig.ID)})
	} else {
		r.JSON(http.StatusInternalServerError, Response{"Entity not created"})
	}
}

// RemoveDatabase
// URL: /api/databases/:id
// Request:
// DELETE
// {
// }
func RemoveDatabase(p martini.Params, r render.Render, db *gorm.DB) {
	dbConfig := DBConfig{}

	// Check that there is a db config with the specified ID.
	if err := db.Where("i_d = ?", p["id"]).First(&dbConfig).Error; err != nil {
		r.JSON(http.StatusNotFound, Response{"Unable to find entry at id: " + p["id"] + " Error: " + err.Error()})
		return
	}

	if dbConfig.ID == 0 {
		r.JSON(http.StatusNotFound, Response{"Unable to find entry at id: " + p["id"]})
		return
	}

	// Delete it.
	if db.Delete(&dbConfig).Error != nil {
		r.JSON(http.StatusInternalServerError, Response{"Can not delete entry for database"})
		return
	}
	r.JSON(http.StatusGone, Response{""})
	return
}
