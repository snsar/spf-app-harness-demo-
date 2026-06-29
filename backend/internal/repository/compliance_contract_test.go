package repository_test

import (
	"github.com/gpsr/backend/internal/repository"
	"github.com/gpsr/backend/internal/service"
)

// Compile-time contract: the MySQL ComplianceRepository must satisfy the service
// layer's persistence port. If the interface and implementation drift, the test
// build fails here — before any runtime call.
var _ service.ComplianceRepository = (*repository.ComplianceRepository)(nil)
