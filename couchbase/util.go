package couchbase

import "gopkg.in/couchbase/gocb.v1"

type CbRole struct {
	Role       string `json:"role"`
	BucketName string `json:"bucket_name"`
}

type CbRoles []CbRole

// this is the statement model for couchbase
// ex;
//	{
//	   "roles": [
//	      {
//		     "role": "bucket_admin",
//			 "bucket_name": "Products"
//	      }
//	   ]
//  }
type CbStatement struct {
	Roles CbRoles `json:"roles"`
}

func (roles CbRoles) ToGocbUserRoles() []gocb.UserRole {
	var userRoles []gocb.UserRole
	for _, r := range []CbRole(roles) {
		userRoles = append(userRoles, gocb.UserRole{
			Role:       r.Role,
			BucketName: r.BucketName,
		})
	}

	return userRoles
}
