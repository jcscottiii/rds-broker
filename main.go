package main

import (
	"github.com/go-martini/martini"
	"github.com/martini-contrib/auth"
	"github.com/martini-contrib/render"
	"github.com/jinzhu/gorm"

	"encoding/json"
	"log"
	"os"
	"strings"
)

type Settings struct {
	EncryptionKey string
	InstanceTags  map[string]string
}

// Loads the RDS object based on the environment variables on a per-plan basis.
// This method will look at <PLAN-NAME>_DB_<VAR>
// Example:
// Plan Name: shared-psql
// Example env variables:
// SHARED-PSQL_DB_TYPE, SHARED-PSQL_DB_URL, SHARED-PSQL_DB_USER, SHARED-PSQL_DB_PASS, SHARED-PSQL_DB_NAME
func LoadRDSFromPlan(plan *Plan) *RDS {
	if plan == nil {
		return nil
	}
	planNameUpper := strings.ToUpper(plan.Name)
	rds := RDS{}
	rds.DbType = os.Getenv(planNameUpper+"_DB_TYPE")
	rds.Url = os.Getenv(planNameUpper +"_DB_URL")
	rds.Username = os.Getenv(planNameUpper+"_DB_USER")
	rds.Password = os.Getenv(planNameUpper+"_DB_PASS")
	rds.DbName = os.Getenv(planNameUpper+"_DB_NAME")
	rds.Sslmode = "verify-ca"

	if rds.Port = os.Getenv(planNameUpper +"_DB_PORT"); rds.Port == "" {
		rds.Port = "5432"
	}

	return &rds
}

func main() {
	var settings Settings
	log.Println("Loading settings")
	var env string = "prod"

	settings.EncryptionKey = os.Getenv("ENC_KEY")
	if settings.EncryptionKey == "" {
		log.Println("An encryption key is required")
		return
	}

	log.Println("Loading app...")
	tags := os.Getenv("INSTANCE_TAGS")
	if tags != "" {
		json.Unmarshal([]byte(tags), &settings.InstanceTags)
	}
	// Connect and collect the pool of shared databases.
	rdsSharedDBPool := &RdsSharedDBPool{Pool: make(map[string]RdsDbConnection)}
	err := rdsSharedDBPool.InitializePoolFromPlans(GetPlans(), env)
	if err != nil {
		log.Println("There was an error with the DB. Error: " + err.Error())
		return
	}

	// Find the plan id corresponding to the database the broker should use for maintaining state of the broker itself.
	rdsDbConnectionForBroker, err := rdsSharedDBPool.FindConnectionByPlanId(os.Getenv("BROKER_DB_PLAN_ID"))
	if err != nil {
		log.Println("Could not find database for broker. Error: " + err.Error())
		return
	}
	DB := rdsDbConnectionForBroker.Conn

	if m := App(&settings, env, DB, rdsSharedDBPool); m != nil {
		log.Println("Starting app...")
		m.Run()
	} else {
		log.Println("Unable to setup application. Exiting...")
	}
}


func App(settings *Settings, env string, DB *gorm.DB, rdsSharedDBPool *RdsSharedDBPool) *martini.ClassicMartini {
	m := martini.Classic()

	username := os.Getenv("AUTH_USER")
	password := os.Getenv("AUTH_PASS")

	m.Use(auth.Basic(username, password))
	m.Use(render.Renderer())

	m.Map(DB)
	m.Map(rdsSharedDBPool)
	m.Map(settings)

	log.Println("Loading Routes")

	// Serve the catalog with services and plans
	m.Get("/v2/catalog", func(r render.Render) {
		services := BuildCatalog()
		catalog := map[string]interface{}{
			"services": services,
		}
		r.JSON(200, catalog)
	})

	// Create the service instance (cf create-service-instance)
	m.Put("/v2/service_instances/:id", CreateInstance)

	// Bind the service to app (cf bind-service)
	m.Put("/v2/service_instances/:instance_id/service_bindings/:id", BindInstance)

	// Unbind the service from app
	m.Delete("/v2/service_instances/:instance_id/service_bindings/:id", func(p martini.Params, r render.Render) {
		var emptyJson struct{}
		r.JSON(200, emptyJson)
	})

	// Delete service instance
	m.Delete("/v2/service_instances/:id", DeleteInstance)

	return m
}
