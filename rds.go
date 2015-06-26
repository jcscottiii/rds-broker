package main

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/rds"
	"github.com/jinzhu/gorm"

	"errors"
	"fmt"
	"log"
)

type RDS struct {
	DbType   string
	Url      string
	Username string
	Password string
	DbName   string
	Sslmode  string
	Port     string
}

type DBInstanceState uint8

const (
	InstanceNotCreated DBInstanceState = iota // 0
	InstanceInProgress                        // 1
	InstanceReady                             // 2
)

type RdsDbConnection struct {
	Rds  *RDS
	Conn *gorm.DB
}

type RdsSharedDBPool struct {
	Pool map[string]RdsDbConnection
}

func (p *RdsSharedDBPool) InitializePoolFromPlans(plans []Plan, env string) error {
	for _, plan := range plans {
		if plan.Adapter == "shared" {
			// Check if plan exists in pool already.
			if _, exists := p.Pool[plan.Id]; exists {
				// Create the error.
				err := errors.New("Unable to initialize plan id (" + plan.Id + ") of plan name (" + plan.Name + "). Already exists.")
				// Log this.
				log.Println(err.Error())
				return err
			} else {
				// Generate RDS for plan.
				Rds := LoadRDSFromPlan(&plan)
				// Initialize DB connection.
				Conn, err := DBInit(Rds)
				if err != nil {
					log.Println("Cannot initialize connection to database for plan: " + plan.Name)
					return err
				}
				rdsDbConnection := RdsDbConnection{Rds: Rds, Conn: Conn}
				p.Pool[plan.Id] = rdsDbConnection
			}
		}
	}
	return nil
}

func (p *RdsSharedDBPool) FindConnectionByPlanId(id string) (*RdsDbConnection, error) {
	if rdsDbConnection, exists := p.Pool[id]; exists {
		return &rdsDbConnection, nil
	}
	return nil, errors.New("Unable to find shared rds connection with plan id: " + id)
}

type IDBAdapterFactory interface {
	CreateDB(plan *Plan, i *Instance, db *gorm.DB, password string) (DBInstanceState, error)
}

type DBAdapterFactory struct {
}

// Main function to create database instances
// Selects an adapter and depending on the plan
// creates the instance
// Returns status and error
// Status codes:
// 0 = not created
// 1 = in progress
// 2 = ready
func (f DBAdapterFactory) CreateDB(plan *Plan,
	i *Instance,
	db *gorm.DB,
	password string) (DBInstanceState, error) {

	var adapter DBAdapter
	switch plan.Adapter {
	case "shared":
		adapter = &SharedDB{
			Db: db,
		}
	case "dedicated":
		adapter = &DedicatedDB{
			InstanceType: plan.InstanceType,
		}
	default:
		return InstanceNotCreated, errors.New("Adapter not found")
	}

	status, err := adapter.CreateDB(i, password)
	return status, err
}

type DBAdapter interface {
	CreateDB(i *Instance, password string) (DBInstanceState, error)
}

type SharedDB struct {
	Db *gorm.DB
}

func (d *SharedDB) CreateDB(i *Instance, password string) (DBInstanceState, error) {
	if db := d.Db.Exec(fmt.Sprintf("CREATE DATABASE %s;", i.Database)); db.Error != nil {
		return InstanceNotCreated, db.Error
	}
	if db := d.Db.Exec(fmt.Sprintf("CREATE USER %s WITH PASSWORD '%s';", i.Username, password)); db.Error != nil {
		return InstanceNotCreated, db.Error
	}
	if db := d.Db.Exec(fmt.Sprintf("GRANT ALL PRIVILEGES ON DATABASE %s TO %s", i.Database, i.Username)); db.Error != nil {
		return InstanceNotCreated, db.Error
	}
	return InstanceReady, nil
}

type DedicatedDB struct {
	InstanceType string
}

func (d *DedicatedDB) CreateDB(i *Instance, password string) (DBInstanceState, error) {
	svc := rds.New(&aws.Config{Region: "us-east-1"})

	var rdsTags []*rds.Tag

	for k, v := range i.Tags {
		rdsTags = append(rdsTags, &rds.Tag{
			Key:   &k,
			Value: &v,
		})
	}

	params := &rds.CreateDBInstanceInput{
		// Everyone gets 10gb for now
		AllocatedStorage: aws.Long(10),
		// Instance class is defined by the plan
		DBInstanceClass:         &d.InstanceType,
		DBInstanceIdentifier:    &i.Database,
		Engine:                  aws.String("postgres"),
		MasterUserPassword:      &i.Password,
		MasterUsername:          &i.Username,
		AutoMinorVersionUpgrade: aws.Boolean(true),
		DBSecurityGroups: []*string{
			aws.String("String"), // Required
			// More values...
		},
		DBSubnetGroupName: aws.String("String"),
		MultiAZ:           aws.Boolean(true),
		StorageEncrypted:  aws.Boolean(true),
		Tags:              rdsTags,
		VPCSecurityGroupIDs: []*string{
			aws.String("String"), // Required
			// More values...
		},
	}
	resp, err := svc.CreateDBInstance(params)

	_ = resp
	_ = err

	// if err != nil {
	// 	if awsErr, ok := err.(awserr.Error); ok {
	// 		// Generic AWS Error with Code, Message, and original error (if any)
	// 		fmt.Println(awsErr.Code(), awsErr.Message(), awsErr.OrigErr())
	// 		if reqErr, ok := err.(awserr.RequestFailure); ok {
	// 			// A service error occurred
	// 			fmt.Println(reqErr.Code(), reqErr.Message(), reqErr.StatusCode(), reqErr.RequestID())
	// 		}
	// 	} else {
	// 		// This case should never be hit, The SDK should alwsy return an
	// 		// error which satisfies the awserr.Error interface.
	// 		fmt.Println(err.Error())
	// 	}
	// }

	// // Pretty-print the response data.
	// fmt.Println(awsutil.StringValue(resp))

	return InstanceNotCreated, nil
}
