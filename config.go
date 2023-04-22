package pglocker

const (
	// PublicScheme is the value of the database schema for
	// using a public database schema.
	PublicScheme = "public"

	// DefaultScheme is the value of the database schema used
	// by default in the configuration.
	DefaultScheme = "pglocker"

	// DefaultTable is the value of the database table used
	// by default in the configuration.
	DefaultTable = "lock_statuses"
)

// Config is a configuration of lock client.
type Config struct {
	// Scheme is a database schema used to store lock statuses.
	// The empty value will be replaced with the default value.
	// The `public` scheme requires explicit set.
	//
	// Default: pglocker.
	Scheme string

	// Table is a database table used to store lock statuses.
	// The empty value will be replaced with the default value.
	//
	// Default: lock_statuses.
	Table string
}

// SetDefault sets default values for fields not specified.
func (c *Config) SetDefault() {
	if c == nil {
		return
	}

	if c.Scheme == "" {
		c.Scheme = DefaultScheme
	}
	if c.Table == "" {
		c.Table = DefaultTable
	}
}
