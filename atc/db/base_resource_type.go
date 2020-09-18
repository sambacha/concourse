package db

import (
	"database/sql"
	"encoding/json"
	"github.com/concourse/concourse/atc"

	sq "github.com/Masterminds/squirrel"
)

// BaseResourceType represents a resource type provided by workers.
//
// It is created via worker registration. All creates are upserts.
//
// It is removed by gc.BaseResourceTypeCollector, once there are no references
// to it from worker_base_resource_types.
type BaseResourceType struct {
	Id   int
	Name string // The name of the type, e.g. 'git'.
}

// UsedBaseResourceType is created whenever a ResourceConfig is used, either
// for a build, a resource in the pipeline, or a resource type in the pipeline.
//
// So long as the UsedBaseResourceType's ID is referenced by a ResourceConfig
// that is in use, this guarantees that the BaseResourceType will not be
// removed. That is to say that its "Use" is vicarious.
type UsedBaseResourceType struct {
	ID                   int    // The ID of the BaseResourceType.
	Name                 string // The name of the type, e.g. 'git'.
	UniqueVersionHistory bool   // If set to true, will create unique version histories for each of the resources using this base resource type
	Defaults             atc.Source
}

// FindOrCreate looks for an existing BaseResourceType and creates it if it
// doesn't exist. It returns a UsedBaseResourceType.
func (brt BaseResourceType) FindOrCreate(tx Tx, unique bool) (*UsedBaseResourceType, error) {
	ubrt, found, err := brt.Find(tx)
	if err != nil {
		return nil, err
	}

	if found && ubrt.UniqueVersionHistory == unique {
		return ubrt, nil
	}

	return brt.create(tx, unique)
}

func (brt BaseResourceType) Find(runner sq.Runner) (*UsedBaseResourceType, bool, error) {
	var id int
	var name string
	var unique bool
	var defaultsString sql.NullString
	sb := psql.Select("id, name, unique_version_history, defaults").
		From("base_resource_types")
	if brt.Id > 0 {
		sb = sb.Where(sq.Eq{"id": brt.Id})
	} else {
		sb = sb.Where(sq.Eq{"name": brt.Name})
	}
	err := sb.Suffix("FOR SHARE").
		RunWith(runner).
		QueryRow().
		Scan(&id, &name, &unique, &defaultsString)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, false, nil
		}

		return nil, false, err
	}

	var defaults atc.Source
	if defaultsString.Valid {
		err := json.Unmarshal([]byte(defaultsString.String), &defaults)
		if err != nil {
			return nil, false, err
		}
	}

	return &UsedBaseResourceType{ID: id, Name: name, UniqueVersionHistory: unique, Defaults: defaults}, true, nil
}

func (brt BaseResourceType) create(tx Tx, unique bool) (*UsedBaseResourceType, error) {
	var id int
	var savedUnique bool
	err := psql.Insert("base_resource_types").
		Columns("name", "unique_version_history").
		Values(brt.Name, unique).
		Suffix(`
			ON CONFLICT (name) DO UPDATE SET
				name = EXCLUDED.name,
				unique_version_history = EXCLUDED.unique_version_history OR base_resource_types.unique_version_history
			RETURNING id, unique_version_history
		`).
		RunWith(tx).
		QueryRow().
		Scan(&id, &savedUnique)
	if err != nil {
		return nil, err
	}

	return &UsedBaseResourceType{ID: id, Name: brt.Name, UniqueVersionHistory: savedUnique}, nil
}
