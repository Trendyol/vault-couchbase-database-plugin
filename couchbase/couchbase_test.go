package couchbase

import (
	"context"
	"errors"
	"fmt"
	"github.com/hashicorp/go-retryablehttp"
	"github.com/hashicorp/vault/sdk/database/dbplugin"
	"github.com/ory/dockertest"
	"github.com/ory/dockertest/docker"
	"gopkg.in/couchbase/gocb.v1"
	"gotest.tools/assert"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	containerName = "test-cb"
	imageName     = "couchbase"
	imageTag      = "community-6.0.0"
	cbUsername    = "admin"
	cbPassword    = "password"
	cbBucketName  = "Test"
)

var conf = map[string]interface{}{
	"connection_string": "couchbase://localhost",
	"username":          cbUsername,
	"password":          cbPassword,
	"bucket":            cbBucketName,
}

var cluster *gocb.Cluster

func TestMain(m *testing.M) {
	pool, err := dockertest.NewPool("")
	if err != nil {
		log.Fatalf("Could not connect to docker: %s", err)
	}

	// remove previously running container if it is still running because cleanup failed
	err = pool.RemoveContainerByName(containerName)
	if err != nil {
		log.Fatalf("Unable to remove old running containers: %s", err)
	}

	// create and run couchbase container
	resource, err := pool.RunWithOptions(&dockertest.RunOptions{
		Name:       containerName,
		Repository: imageName,
		Tag:        imageTag,
		PortBindings: map[docker.Port][]docker.PortBinding{
			"8091/tcp":  {{HostIP: "0.0.0.0", HostPort: "8091"}},
			"8092/tcp":  {{HostIP: "0.0.0.0", HostPort: "8092"}},
			"8093/tcp":  {{HostIP: "0.0.0.0", HostPort: "8093"}},
			"8094/tcp":  {{HostIP: "0.0.0.0", HostPort: "8094"}},
			"12210/tcp": {{HostIP: "0.0.0.0", HostPort: "12210"}},
		},
	})
	if err != nil {
		log.Fatalf("Could not start couchbase container: %s", err)
	}

	// exponential backoff-retry, because container might not be ready to accept connections yet
	if err := pool.Retry(func() error {
		var err error
		cluster, err = gocb.Connect("couchbase://localhost")
		return err
	}); err != nil {
		log.Fatalf("could not connect to couchbase container: %s", err)
	}

	if err := configureCouchbaseCluster(); err != nil {
		log.Fatalf("could not configure couchbase cluster: %s", err)
	}

	if err := cluster.Authenticate(gocb.PasswordAuthenticator{Username: cbUsername, Password: cbPassword,}); err != nil {
		log.Fatalf("could not authenticate to couchbase: %s", err)
	}

	// couchbase returns Accepted and creates buckets asynchronously
	// so we need to make sure bucket is ready before running tests
	if err := createBucket(); err != nil {
		log.Fatalf("could not create bucket: %s", err)
	}

	// to make sure bucket is created and ready, trying to open it with retry
	var bucket *gocb.Bucket
	if err := pool.Retry(func() error {
		var err error
		bucket, err = cluster.OpenBucket(cbBucketName, "")
		return err
	}); err != nil {
		log.Fatalf("could not open bucket: %s", err)
	}

	// opened the bucket just to be sure that it's created successfully, now closing it
	if err = bucket.Close(); err != nil {
		log.Fatalf("could not close the bucket: %s", err)
	}

	// cluster and bucket is ready, run the tests
	code := m.Run()

	// cleanup couchbase container
	if err := pool.Purge(resource); err != nil {
		log.Fatalf("could not purge container: %s", err)
	}

	os.Exit(code)
}

func TestCouchbase_Init(t *testing.T) {
	cb := new()
	_, err := cb.Init(context.Background(), conf, true)
	assert.NilError(t, err)

	assert.Equal(t, cb.Initialized, true)
	assert.Equal(t, cb.Bucket, cbBucketName)
	assert.Equal(t, cb.Username, cbUsername)
	assert.Equal(t, cb.Password, cbPassword)
	assert.Equal(t, cb.ConnectionString, "couchbase://localhost")
}

func TestCouchbase_CreateUser(t *testing.T) {
	cb := new()
	_, err := cb.Init(context.Background(), conf, true)
	assert.NilError(t, err)

	// create a user with role bucket_full_access on bucket Test
	username, password, err := cb.CreateUser(context.Background(), dbplugin.Statements{
		Creation: []string{fmt.Sprintf("{\"roles\": [{\"role\": \"bucket_full_access\",\"bucket_name\": \"%s\"}]}", cbBucketName)},
	}, dbplugin.UsernameConfig{DisplayName: "test-user", RoleName: "test-role"}, time.Now().Add(time.Hour))
	assert.NilError(t, err)

	user, err := cb.clusterManager.GetUser("local", username)
	assert.NilError(t, err)

	err = cb.cluster.Authenticate(gocb.PasswordAuthenticator{
		Username: username,
		Password: password,
	})
	assert.NilError(t, err)

	assert.Equal(t, user.Name, username)
	assert.Equal(t, user.Roles[0].BucketName, cbBucketName)
	assert.Equal(t, user.Roles[0].Role, "bucket_full_access")
}

func TestCouchbase_RevokeUser(t *testing.T) {
	cb := new()
	_, err := cb.Init(context.Background(), conf, true)
	assert.NilError(t, err)

	username, _, err := cb.CreateUser(context.Background(), dbplugin.Statements{
		Creation: []string{fmt.Sprintf("{\"roles\": [{\"role\": \"bucket_full_access\",\"bucket_name\": \"%s\"}]}", cbBucketName)},
	}, dbplugin.UsernameConfig{DisplayName: "test-user", RoleName: "test-role"}, time.Now().Add(time.Hour))
	assert.NilError(t, err)

	err = cb.RevokeUser(context.Background(), dbplugin.Statements{}, username)
	assert.NilError(t, err)

	_, err = cb.clusterManager.GetUser("local", username)
	assert.Error(t, err, "\"Unknown user.\"")
}

func TestCouchbase_SetCredentials(t *testing.T) {
	cb := new()
	_, err := cb.Init(context.Background(), conf, true)
	assert.NilError(t, err)

	username, password, err := cb.SetCredentials(context.Background(), dbplugin.Statements{
		Creation: []string{fmt.Sprintf("{\"roles\": [{\"role\": \"bucket_full_access\",\"bucket_name\": \"%s\"}]}", cbBucketName)},
	}, dbplugin.StaticUserConfig{
		Username: "test-user",
		Password: "test-password",
	})

	user, err := cb.clusterManager.GetUser("local", username)
	assert.NilError(t, err)

	err = cb.cluster.Authenticate(gocb.PasswordAuthenticator{
		Username: username,
		Password: password,
	})
	assert.NilError(t, err)

	assert.Equal(t, user.Name, username)
	assert.Equal(t, user.Roles[0].BucketName, cbBucketName)
	assert.Equal(t, user.Roles[0].Role, "bucket_full_access")
}

func configureCouchbaseCluster() error {
	// setup services
	if resp, err := postFormWithRetry("http://localhost:8091/node/controller/setupServices", url.Values{
		"services": {"kv"}, // only kv service needed
	}); err != nil {
		return err
	} else if resp.StatusCode != 200 {
		return errors.New(fmt.Sprintf("could not setup services. response status: %d", resp.StatusCode))
	}

	// set memory quota
	if resp, err := postFormWithRetry("http://localhost:8091/pools/default", url.Values{
		"memoryQuota": {"256"},
	}); err != nil {
		return err
	} else if resp.StatusCode != 200 {
		return errors.New(fmt.Sprintf("could not setup memory quota. response status: %d", resp.StatusCode))
	}

	// setup node
	if resp, err := postFormWithRetry("http://localhost:8091/nodes/self/controller/settings", url.Values{
		"path":       {"/opt/couchbase/var/lib/couchbase/data"},
		"index_path": {"/opt/couchbase/var/lib/couchbase/data"},
	}); err != nil {
		return err
	} else if resp.StatusCode != 200 {
		return errors.New(fmt.Sprintf("could not setup node. response status: %d", resp.StatusCode))
	}

	// set admin username and password
	if resp, err := postFormWithRetry("http://localhost:8091/settings/web", url.Values{
		"username": {cbUsername},
		"password": {cbPassword},
		"port":     {"SAME"},
	}); err != nil {
		return err
	} else if resp.StatusCode != 200 {
		return errors.New(fmt.Sprintf("could not set admin username and password on couchbase. response status: %d", resp.StatusCode))
	}

	return nil
}

func createBucket() error {
	if resp, err := postFormWithRetry("http://localhost:8091/pools/default/buckets", url.Values{
		"bucketType":    {"couchbase"},
		"name":          {cbBucketName},
		"ramQuotaMB":    {"256"},
		"replicaNumber": {"0"},
	}); err != nil {
		return err
	} else if resp.StatusCode != 202 {
		return errors.New(fmt.Sprintf("could not create test bucket. response status: %d", resp.StatusCode))
	}

	return nil
}

func postFormWithRetry(url string, form url.Values) (*http.Response, error) {
	req, err := retryablehttp.NewRequest(http.MethodPost, url, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(cbUsername, cbPassword)

	client := retryablehttp.NewClient()
	client.RetryMax = 10
	client.RetryWaitMin = time.Second
	client.RetryWaitMax = time.Second * 5

	return client.Do(req)
}
