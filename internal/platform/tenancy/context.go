package tenancy

import (
	"errors"

	"github.com/google/uuid"
)

type AccessMode string

const (
	AccessModeTenant   AccessMode = "TENANT"
	AccessModePlatform AccessMode = "PLATFORM"
)

type Context struct {
	Mode     AccessMode
	TenantID *uuid.UUID
}

func Tenant(companyID uuid.UUID) Context {
	return Context{Mode: AccessModeTenant, TenantID: &companyID}
}

func Platform() Context {
	return Context{Mode: AccessModePlatform}
}

func (c Context) Validate() error {
	switch c.Mode {
	case AccessModeTenant:
		if c.TenantID == nil || *c.TenantID == uuid.Nil {
			return errors.New("tenant context requires a company id")
		}
	case AccessModePlatform:
		if c.TenantID != nil {
			return errors.New("platform context cannot contain a company id")
		}
	default:
		return errors.New("unsupported access mode")
	}
	return nil
}
