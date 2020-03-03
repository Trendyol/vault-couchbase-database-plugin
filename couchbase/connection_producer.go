package couchbase

import (
	"context"
	"fmt"
	"github.com/hashicorp/errwrap"
	"github.com/hashicorp/vault/sdk/database/helper/connutil"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"gopkg.in/couchbase/gocb.v1"
	"sync"
	"time"
)

// couchbaseConnectionProducer implements ConnectionProducer
type couchbaseConnectionProducer struct {
	ConnectionString string `json:"connection_string" structs:"connection_string" mapstructure:"connection_string"`
	Username         string `json:"username" structs:"username" mapstructure:"username"`
	Password         string `json:"password" structs:"password" mapstructure:"password"`
	Bucket           string `json:"bucket" structs:"bucket" mapstructure:"bucket"`

	Type        string
	Initialized bool
	RawConfig   map[string]interface{}

	cluster        *gocb.Cluster
	clusterManager *gocb.ClusterManager
	sync.Mutex
}

func (c *couchbaseConnectionProducer) Initialize(ctx context.Context, conf map[string]interface{}, verifyConnection bool) error {
	_, err := c.Init(ctx, conf, verifyConnection)
	return err
}

func (c *couchbaseConnectionProducer) Init(ctx context.Context, conf map[string]interface{}, verifyConnection bool) (map[string]interface{}, error) {
	c.Lock()
	defer c.Unlock()

	c.RawConfig = conf

	err := mapstructure.WeakDecode(conf, c)
	if err != nil {
		return nil, err
	}

	if len(c.ConnectionString) == 0 {
		return nil, fmt.Errorf("connection_string cannot be empty")
	}
	if len(c.Username) == 0 {
		return nil, fmt.Errorf("username cannot be empty")
	}
	if len(c.Password) == 0 {
		return nil, fmt.Errorf("password cannot be empty")
	}

	c.Initialized = true

	if verifyConnection {
		if _, err := c.Connection(ctx); err != nil {
			return nil, errwrap.Wrapf("error verifying connection: {{err}}", err)
		}
	}

	return conf, nil
}

func (c *couchbaseConnectionProducer) Connection(context.Context) (interface{}, error) {
	if !c.Initialized {
		return nil, connutil.ErrNotInitialized
	}

	cluster, err := gocb.Connect(c.ConnectionString)
	if err != nil {
		return nil, err
	}

	cluster.SetConnectTimeout(time.Second * 30)
	cluster.SetServerConnectTimeout(time.Second * 30)

	if err = cluster.Authenticate(gocb.PasswordAuthenticator{Username: c.Username, Password: c.Password}); err != nil {
		return nil, err
	}

	// must open a bucket in order to perform cluster level operations
	if _, err = cluster.OpenBucket(c.Bucket, ""); err != nil {
		return nil, errors.Wrapf(err, "could not open bucket %s", c.Bucket)
	}

	clusterManager := cluster.Manager(c.Username, c.Password)

	c.cluster = cluster
	c.clusterManager = clusterManager

	return c.cluster, nil
}

func (c *couchbaseConnectionProducer) Close() error {
	c.Lock()
	defer c.Unlock()

	if c.cluster != nil {
		if err := c.cluster.Close(); err != nil {
			return err
		}
	}

	c.cluster = nil
	return nil
}

func (c *couchbaseConnectionProducer) secretValues() map[string]interface{} {
	return map[string]interface{}{
		c.Password: "[password]",
	}
}
