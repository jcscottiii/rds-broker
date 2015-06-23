package main

import (
	"github.com/go-martini/martini"
	"github.com/martini-contrib/auth"
	"github.com/martini-contrib/render"

	"encoding/json"
	"log"
	"os"
)

type RDS struct {
	Url      string
	Username string
	Password string
	DbName   string
	Sslmode  string
	Port     string
}

type Settings struct {
	EncryptionKey string
	Rds           *RDS
	InstanceTags  map[string]string
}

func LoadRDS() *RDS {
	rds := RDS{}
	rds.Url = os.Getenv("DB_URL")
	rds.Username = os.Getenv("DB_USER")
	rds.Password = os.Getenv("DB_PASS")
	rds.DbName = os.Getenv("DB_NAME")
	rds.Sslmode = "verify-ca"

	if os.Getenv("DB_PORT") != "" {
		rds.Port = os.Getenv("DB_PORT")
	} else {
		rds.Port = "5432"
	}

	return &rds
}

func main() {
	var settings Settings
	log.Println("Loading settings")
	settings.Rds = LoadRDS()

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

	if m := App(&settings, "prod"); m != nil {
		log.Println("Starting app...")
		m.Run()
	} else {
		log.Println("Unable to setup application. Exiting...")
	}
}

func App(settings *Settings, env string) *martini.ClassicMartini {

	err := DBInit(settings.Rds, env)
	if err != nil {
		log.Println("There was an error with the DB. Error: " + err.Error())
		return nil
	}

	m := martini.Classic()

	username := os.Getenv("AUTH_USER")
	password := os.Getenv("AUTH_PASS")

	m.Use(auth.Basic(username, password))
	m.Use(render.Renderer())

	m.Map(&DB)
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
