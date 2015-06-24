package main

import (
	"github.com/jinzhu/gorm"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"

	"errors"
	"fmt"
	"log"
)

// Connection string parameters for Postgres - http://godoc.org/github.com/lib/pq, if you are using another
// database refer to the relevant driver's documentation.

// * dbname - The name of the database to connect to
// * user - The user to sign in as
// * password - The user's password
// * host - The host to connect to. Values that start with / are for unix domain sockets.
//   (default is localhost)
// * port - The port to bind to. (default is 5432)
// * sslmode - Whether or not to use SSL (default is require, this is not the default for libpq)
//   Valid SSL modes:
//    * disable - No SSL
//    * require - Always SSL (skip verification)
//    * verify-full - Always SSL (require verification)

// This is to connect to Shared DB Instances.
// TODO Rename method name.
func DBInit(rds *RDS) (*gorm.DB, error) {
	var err error
	var DB gorm.DB
	switch rds.DbType {
	case "postgres":
		conn := "dbname=%s user=%s password=%s host=%s sslmode=%s port=%s"
		conn = fmt.Sprintf(conn,
			rds.DbName,
			rds.Username,
			rds.Password,
			rds.Url,
			rds.Sslmode,
			rds.Port)
		DB, err = gorm.Open("postgres", conn)
	case "sqlite3":
		DB, err = gorm.Open("sqlite3", rds.DbName)
	default:
		errorString := "Cannot connect. Unsupported DB type: (" + rds.DbType + ")"
		log.Println(errorString)
		return nil, errors.New(errorString)
	}
	if err != nil {
		log.Println("Error!")
		return nil, err
	}

	if err = DB.DB().Ping(); err != nil {
		log.Println("Unable to verify connection to database")
		return nil, err
	}
	DB.DB().SetMaxOpenConns(10)
	log.Println("Migrating")
	// Automigrate!
	DB.AutoMigrate(Instance{})
	log.Println("Migrated")
	return &DB, nil
}
