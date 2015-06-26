package main

import (
	"github.com/go-martini/martini"
	"github.com/jinzhu/gorm"

	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

type MockDBAdapterFactory struct {
}
// Main function to create database instances
// Selects an adapter and depending on the plan
// creates the instance
// Returns status and error
// Status codes:
// 0 = not created
// 1 = in progress
// 2 = ready
func (f MockDBAdapterFactory) CreateDB(plan *Plan,
	i *Instance,
	db *gorm.DB,
	password string) (DBInstanceState, error) {

	var adapter DBAdapter
	switch plan.Adapter {
	case "shared":
		adapter = &MockSharedDB{
			Db: db,
		}
	case "dedicated":
		adapter = &MockDedicatedDB{
			InstanceType: plan.InstanceType,
		}
	default:
		return InstanceNotCreated, errors.New("Adapter not found")
	}

	status, err := adapter.CreateDB(i, password)
	return status, err
}

type MockSharedDB struct {
	Db *gorm.DB
}

func (d *MockSharedDB) CreateDB(i *Instance, password string) (DBInstanceState, error) {
	/*
	if db := d.Db.Exec(fmt.Sprintf("CREATE DATABASE %s;", i.Database)); db.Error != nil {
		return InstanceNotCreated, db.Error
	}
	if db := d.Db.Exec(fmt.Sprintf("CREATE USER %s WITH PASSWORD '%s';", i.Username, password)); db.Error != nil {
		return InstanceNotCreated, db.Error
	}
	if db := d.Db.Exec(fmt.Sprintf("GRANT ALL PRIVILEGES ON DATABASE %s TO %s", i.Database, i.Username)); db.Error != nil {
		return InstanceNotCreated, db.Error
	}
	*/
	return InstanceReady, nil
}

type MockDedicatedDB struct {
	InstanceType string
}

func (d *MockDedicatedDB) CreateDB(i *Instance, password string) (DBInstanceState, error) {
	return InstanceReady, nil
}

var createInstanceReq []byte = []byte(`{
	"service_id":"the-service",
	"plan_id":"44d24fc7-f7a4-4ac1-b7a0-de82836e89a3",
	"organization_guid":"an-org",
	"space_guid":"a-space"
}`)

var bindInstanceReq []byte = []byte(`{
	"service_id":"the-service",
	"plan_id":"44d24fc7-f7a4-4ac1-b7a0-de82836e89a3",
	"app_guid":"an-app"
}`)

func setup(DB *gorm.DB, sharedPool *RdsSharedDBPool) *martini.ClassicMartini {
	os.Setenv("AUTH_USER", "default")
	os.Setenv("AUTH_PASS", "default")
	var s Settings
	s.DBAdapterFactoryInstance = MockDBAdapterFactory{}
	s.EncryptionKey = "12345678901234567890123456789012"

	m := App(&s, "test", DB, sharedPool)

	return m
}

func setupDB() (*gorm.DB, *RdsSharedDBPool) {
	var r RDS
	r.DbType = "sqlite3"
	r.DbName = ":memory:"
	conn, _ := DBInit(&r)
	rdsDbConnection := RdsDbConnection{}
	rdsDbConnection.Conn = conn
	rdsDbConnection.Rds = &r
	sharedPool := &RdsSharedDBPool{Pool: make(map[string]RdsDbConnection)}
	sharedPool.Pool["44d24fc7-f7a4-4ac1-b7a0-de82836e89a3"] = rdsDbConnection
	return conn, sharedPool
}

func doRequest(m *martini.ClassicMartini, url string, method string, auth bool, body io.Reader, DB *gorm.DB, Pool *RdsSharedDBPool) (*httptest.ResponseRecorder, *martini.ClassicMartini) {
	if m == nil {
		m = setup(DB, Pool)
	}

	res := httptest.NewRecorder()
	req, _ := http.NewRequest(method, url, body)
	if auth {
		req.SetBasicAuth("default", "default")
	}

	m.ServeHTTP(res, req)

	return res, m
}

func validJson(response []byte, url string, t *testing.T) {
	var aJson map[string]interface{}
	if json.Unmarshal(response, &aJson) != nil {
		t.Error(url, "should return a valid json")
	}
}

func TestCatalog(t *testing.T) {
	DB, Pool := setupDB()
	url := "/v2/catalog"
	res, _ := doRequest(nil, url, "GET", false, nil, DB, Pool)

	// Without auth
	if res.Code != http.StatusUnauthorized {
		t.Error(url, "without auth should return 401")
	}

	res, _ = doRequest(nil, url, "GET", true, nil, DB, Pool)

	// With auth
	if res.Code != http.StatusOK {
		t.Error(url, "with auth should return 200 and it returned", res.Code)
	}

	// Is it a valid JSON?
	validJson(res.Body.Bytes(), url, t)
}

func TestCreateInstance(t *testing.T) {
	DB, Pool := setupDB()
	url := "/v2/service_instances/the_instance"

	res, _ := doRequest(nil, url, "PUT", true, bytes.NewBuffer(createInstanceReq), DB, Pool)

	if res.Code != http.StatusCreated {
		t.Logf("Unable to create instance. Body is: " + res.Body.String())
		t.Error(url, "with auth should return 201 and it returned", res.Code)
	}

	// Is it a valid JSON?
	validJson(res.Body.Bytes(), url, t)

	// Does it say "created"?
	if !strings.Contains(string(res.Body.Bytes()), "created") {
		t.Error(url, "should return the instance created message")
	}

	// Is it in the database and has a username and password?
	i := Instance{}
	DB.Where("uuid = ?", "the_instance").First(&i)
	if i.Id == 0 {
		t.Error("The instance should be saved in the DB")
	}

	if i.Username == "" || i.Password == "" {
		t.Error("The instance should have a username and password")
	}

	if i.PlanId == "" || i.OrgGuid == "" || i.SpaceGuid == "" {
		t.Error("The instance should have metadata")
	}
}

func TestBindInstance(t *testing.T) {
	validJson(bindInstanceReq, "jamesurl", t)
	DB, Pool := setupDB()
	url := "/v2/service_instances/the_instance/service_bindings/the_binding"
	res, m := doRequest(nil, url, "PUT", true, bytes.NewBuffer(bindInstanceReq), DB, Pool)

	// Without the instance
	if res.Code != http.StatusNotFound {
		t.Error(url, "with auth should return 404 and it returned", res.Code)
	}

	// Create the instance and try again
	doRequest(m, "/v2/service_instances/the_instance", "PUT", true, bytes.NewBuffer(createInstanceReq), DB, Pool)

	res, _ = doRequest(m, url, "PUT", true, bytes.NewBuffer(bindInstanceReq), DB, Pool)
	if res.Code != http.StatusCreated {
		t.Logf("Unable to create instance. Body is: " + res.Body.String())
		t.Error(url, "with auth should return 201 and it returned", res.Code)
	}

	// Is it a valid JSON?
	validJson(res.Body.Bytes(), url, t)

	type credentials struct {
		Uri      string
		Username string
		Password string
		Host     string
		DbName   string
	}

	type response struct {
		Credentials credentials
	}

	var r response

	json.Unmarshal(res.Body.Bytes(), &r)

	// Does it contain "uri"
	if r.Credentials.Uri == "" {
		t.Error(url, "should return credentials")
	}

	instance := Instance{}
	DB.Where("uuid = ?", "the_instance").First(&instance)

	// Does it return an unencrypted password?
	if instance.Password == r.Credentials.Password || r.Credentials.Password == "" {
		t.Error(url, "should return an unencrypted password and it returned", r.Credentials.Password)
	}
}

func TestUnbind(t *testing.T) {
	DB, Pool := setupDB()
	url := "/v2/service_instances/the_instance/service_bindings/the_binding"
	res, _ := doRequest(nil, url, "DELETE", true, nil, DB, Pool)

	if res.Code != http.StatusOK {
		t.Error(url, "with auth should return 200 and it returned", res.Code)
	}

	// Is it a valid JSON?
	validJson(res.Body.Bytes(), url, t)

	// Is it an empty object?
	if string(res.Body.Bytes()) != "{}" {
		t.Error(url, "should return an empty JSON")
	}
}

func TestDeleteInstance(t *testing.T) {
	DB, Pool := setupDB()
	url := "/v2/service_instances/the_instance"
	res, m := doRequest(nil, url, "DELETE", true, nil, DB, Pool)

	// With no instance
	if res.Code != http.StatusNotFound {
		t.Error(url, "with auth should return 404 and it returned", res.Code)
	}

	// Create the instance and try again
	doRequest(m, "/v2/service_instances/the_instance", "PUT", true, bytes.NewBuffer(createInstanceReq), DB, Pool)
	i := Instance{}
	DB.Where("uuid = ?", "the_instance").First(&i)
	if i.Id == 0 {
		t.Error("The instance should be in the DB")
	}

	res, _ = doRequest(m, url, "DELETE", true, nil, DB, Pool)

	if res.Code != http.StatusOK {
		t.Logf("Unable to create instance. Body is: " + res.Body.String())
		t.Error(url, "with auth should return 200 and it returned", res.Code)
	}

	// Is it actually gone from the DB?
	i = Instance{}
	DB.Where("uuid = ?", "the_instance").First(&i)
	if i.Id > 0 {
		t.Error("The instance shouldn't be in the DB")
	}
}
