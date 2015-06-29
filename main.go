package main

import (
	"github.com/go-martini/martini"
	"github.com/jinzhu/gorm"
	"github.com/martini-contrib/auth"
	"github.com/martini-contrib/render"

	"encoding/json"
	"log"
	"os"
	"strings"
)

type Settings struct {
	EncryptionKey            string
	InstanceTags             map[string]string
	DBAdapterFactoryInstance IDBAdapterFactory
}

// Loads the RDS object based on the environment variables on a per-plan basis.
// This method will look at <PLAN_NAME>_DB_<VAR>
// Example:
// Plan Name: shared-psql
// Example env variables:
// SHARED_PSQL_DB_TYPE, SHARED_PSQL_DB_URL, SHARED_PSQL_DB_USER, SHARED_PSQL_DB_PASS, SHARED_PSQL_DB_NAME
func LoadRDSFromPlan(plan *Plan) *RDS {
	if plan == nil {
		return nil
	}
	// Make the plan name all capitalized.
	planNameUpper := strings.ToUpper(plan.Name)
	// Make the plan name compliant with Linux env variable naming (no '-' allowed).
	planNameUpper = strings.Replace(planNameUpper, "-", "_", -1)
	rds := RDS{}
	rds.DbType = os.Getenv(planNameUpper + "_DB_TYPE")
	rds.Url = os.Getenv(planNameUpper + "_DB_URL")
	rds.Username = os.Getenv(planNameUpper + "_DB_USER")
	rds.Password = os.Getenv(planNameUpper + "_DB_PASS")
	rds.DbName = os.Getenv(planNameUpper + "_DB_NAME")
	if rds.Sslmode = os.Getenv(planNameUpper + "_DB_SSLMODE"); rds.Sslmode == "" {
		rds.Sslmode = "verify-ca"
	}

	if rds.Port = os.Getenv(planNameUpper + "_DB_PORT"); rds.Port == "" {
		rds.Port = "5432"
	}

	return &rds
}

func main() {
	// Add settings.
	var settings Settings
	log.Println("Loading settings")
	// Set the environment string.
	var env string = "prod"

	// Get the encryption key.
	settings.EncryptionKey = os.Getenv("ENC_KEY")
	if settings.EncryptionKey == "" {
		log.Println("An encryption key is required")
		return
	}

	// Set the type of DB Adapter Factory.
	settings.DBAdapterFactoryInstance = DBAdapterFactory{}

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
