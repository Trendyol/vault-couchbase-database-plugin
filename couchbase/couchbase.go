package couchbase

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/hashicorp/vault/api"
	"github.com/hashicorp/vault/sdk/database/dbplugin"
	"github.com/hashicorp/vault/sdk/database/helper/credsutil"
	"github.com/hashicorp/vault/sdk/database/helper/dbutil"
	"github.com/pkg/errors"
	"gopkg.in/couchbase/gocb.v1"
	"time"
)

const (
	couchbaseTypeName = "couchbase"
	authDomain        = "local"
)

// Couchbase is an implementation of Database interface
type Couchbase struct {
	*couchbaseConnectionProducer
	credsutil.CredentialsProducer
}

var _ dbplugin.Database = &Couchbase{}

func Run(apiTLSConfig *api.TLSConfig) error {
	dbType, err := New()
	if err != nil {
		return err
	}

	dbplugin.Serve(dbType.(dbplugin.Database), api.VaultPluginTLSProvider(apiTLSConfig))

	return nil
}

func New() (interface{}, error) {
	db := new()
	dbType := dbplugin.NewDatabaseErrorSanitizerMiddleware(db, db.secretValues)

	return dbType, nil
}

// New returns a new Couchbase instance
func new() *Couchbase {
	connProducer := &couchbaseConnectionProducer{}
	connProducer.Type = couchbaseTypeName

	credsProducer := &credsutil.SQLCredentialsProducer{
		DisplayNameLen: 15,
		RoleNameLen:    15,
		UsernameLen:    100,
		Separator:      "_",
	}

	return &Couchbase{
		couchbaseConnectionProducer: connProducer,
		CredentialsProducer:         credsProducer,
	}
}

func (c *Couchbase) Type() (string, error) {
	return couchbaseTypeName, nil
}

// Generates username and password, creates a user in the database with those credentials
func (c *Couchbase) CreateUser(ctx context.Context, statements dbplugin.Statements, usernameConfig dbplugin.UsernameConfig, expiration time.Time) (username string, password string, err error) {
	c.Lock()
	defer c.Unlock()

	statements = dbutil.StatementCompatibilityHelper(statements)

	if len(statements.Creation) == 0 {
		return "", "", dbutil.ErrEmptyCreationStatement
	}

	_, err = c.Connection(ctx)
	if err != nil {
		return "", "", err
	}

	username, err = c.GenerateUsername(usernameConfig)
	if err != nil {
		return "", "", err
	}

	password, err = c.GeneratePassword()
	if err != nil {
		return "", "", err
	}

	return upsertUser(c.clusterManager, statements.Creation[0], username, password)
}

// Sets or creates a user with the given username and password
func (c *Couchbase) SetCredentials(ctx context.Context, statements dbplugin.Statements, staticConfig dbplugin.StaticUserConfig) (username string, password string, err error) {
	c.Lock()
	defer c.Unlock()

	statements = dbutil.StatementCompatibilityHelper(statements)

	if len(statements.Creation) == 0 {
		return "", "", dbutil.ErrEmptyCreationStatement
	}

	_, err = c.Connection(ctx)
	if err != nil {
		return "", "", err
	}

	return upsertUser(c.clusterManager, statements.Creation[0], staticConfig.Username, staticConfig.Password)
}

// not supported in couchbase
func (c *Couchbase) RenewUser(ctx context.Context, statements dbplugin.Statements, username string, expiration time.Time) error {
	return nil
}

// deletes user with the given username
func (c *Couchbase) RevokeUser(ctx context.Context, statements dbplugin.Statements, username string) error {
	c.Lock()
	defer c.Unlock()

	_, err := c.Connection(ctx)
	if err != nil {
		return err
	}

	return c.clusterManager.RemoveUser(authDomain, username)
}

// not supported in couchbase
func (c *Couchbase) RotateRootCredentials(ctx context.Context, statements []string) (config map[string]interface{}, err error) {
	return nil, errors.New("root credential rotation is not currently implemented in couchbase")
}

func upsertUser(clusterManager *gocb.ClusterManager, creationStatement string, username string, password string) (string, string, error) {
	var cbStatement CbStatement
	err := json.Unmarshal([]byte(creationStatement), &cbStatement)
	if err != nil {
		return "", "", errors.Wrap(err, "invalid creation statement")
	}

	if len(cbStatement.Roles) == 0 {
		return "", "", fmt.Errorf("at least one role should be given in creation statement")
	}

	if err = clusterManager.UpsertUser(authDomain, username, &gocb.UserSettings{
		Name:     username,
		Password: password,
		Roles:    cbStatement.Roles.ToGocbUserRoles(),
	}); err != nil {
		return "", "", errors.Wrap(err, "error when upserting user")
	}

	return username, password, nil
}
