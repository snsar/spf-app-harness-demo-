package repository

import (
	"errors"

	"github.com/go-sql-driver/mysql"
)

// ErrReferenced is returned when a delete is refused by a RESTRICT foreign key
// (MySQL error 1451): the row is still referenced by another (e.g. an entity or
// warning_template referenced by a classification_rule — C5). Handlers map this
// to HTTP 409 Conflict.
var ErrReferenced = errors.New("row is referenced by another row")

// mysqlRowReferenced reports whether err is the MySQL 1451 FK-RESTRICT violation.
func mysqlRowReferenced(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1451
}
