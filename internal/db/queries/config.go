// Package queries contains database query functions.
package queries

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willibrandon/steep/internal/db/models"
)

// GetAllParameters retrieves all configuration parameters from pg_settings.
func GetAllParameters(ctx context.Context, pool *pgxpool.Pool) (*models.ConfigData, error) {
	const query = `
SELECT
    name,
    setting,
    COALESCE(unit, '') AS unit,
    category,
    short_desc,
    COALESCE(extra_desc, '') AS extra_desc,
    context,
    vartype,
    source,
    COALESCE(min_val, '') AS min_val,
    COALESCE(max_val, '') AS max_val,
    enumvals,
    COALESCE(boot_val, '') AS boot_val,
    COALESCE(reset_val, '') AS reset_val,
    COALESCE(sourcefile, '') AS sourcefile,
    COALESCE(sourceline, 0) AS sourceline,
    COALESCE(pending_restart, false) AS pending_restart
FROM pg_settings
ORDER BY name`

	rows, err := pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	data := models.NewConfigData()
	categorySet := make(map[string]struct{})

	for rows.Next() {
		var p models.Parameter
		err := rows.Scan(
			&p.Name,
			&p.Setting,
			&p.Unit,
			&p.Category,
			&p.ShortDesc,
			&p.ExtraDesc,
			&p.Context,
			&p.VarType,
			&p.Source,
			&p.MinVal,
			&p.MaxVal,
			&p.EnumVals,
			&p.BootVal,
			&p.ResetVal,
			&p.SourceFile,
			&p.SourceLine,
			&p.PendingRestart,
		)
		if err != nil {
			return nil, err
		}

		data.Parameters = append(data.Parameters, p)

		// Track modified and pending restart counts
		if p.IsModified() {
			data.ModifiedCount++
		}
		if p.PendingRestart {
			data.PendingRestartCount++
		}

		// Collect unique top-level categories
		topCat := p.TopLevelCategory()
		if _, exists := categorySet[topCat]; !exists {
			categorySet[topCat] = struct{}{}
			data.Categories = append(data.Categories, topCat)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return data, nil
}

// GetParameter retrieves a single parameter by name.
func GetParameter(ctx context.Context, pool *pgxpool.Pool, name string) (*models.Parameter, error) {
	const query = `
SELECT
    name,
    setting,
    COALESCE(unit, '') AS unit,
    category,
    short_desc,
    COALESCE(extra_desc, '') AS extra_desc,
    context,
    vartype,
    source,
    COALESCE(min_val, '') AS min_val,
    COALESCE(max_val, '') AS max_val,
    enumvals,
    COALESCE(boot_val, '') AS boot_val,
    COALESCE(reset_val, '') AS reset_val,
    COALESCE(sourcefile, '') AS sourcefile,
    COALESCE(sourceline, 0) AS sourceline,
    COALESCE(pending_restart, false) AS pending_restart
FROM pg_settings
WHERE name = $1`

	var p models.Parameter
	err := pool.QueryRow(ctx, query, name).Scan(
		&p.Name,
		&p.Setting,
		&p.Unit,
		&p.Category,
		&p.ShortDesc,
		&p.ExtraDesc,
		&p.Context,
		&p.VarType,
		&p.Source,
		&p.MinVal,
		&p.MaxVal,
		&p.EnumVals,
		&p.BootVal,
		&p.ResetVal,
		&p.SourceFile,
		&p.SourceLine,
		&p.PendingRestart,
	)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// AlterSystemSet changes a configuration parameter using ALTER SYSTEM SET.
// The value will be written to postgresql.auto.conf.
func AlterSystemSet(ctx context.Context, pool *pgxpool.Pool, parameter, value string) error {
	// Use ALTER SYSTEM SET ... TO syntax without quoting the value.
	// PostgreSQL will handle the quoting when writing to postgresql.auto.conf.
	// This avoids double-quoting issues with comma-separated list values.
	query := "ALTER SYSTEM SET " + parameter + " TO " + value
	_, err := pool.Exec(ctx, query)
	return err
}

// AlterSystemReset resets a configuration parameter to its default using ALTER SYSTEM RESET.
// This removes the parameter from postgresql.auto.conf.
func AlterSystemReset(ctx context.Context, pool *pgxpool.Pool, parameter string) error {
	query := "ALTER SYSTEM RESET " + pgx.Identifier{parameter}.Sanitize()
	_, err := pool.Exec(ctx, query)
	return err
}

// ReloadConfig reloads PostgreSQL configuration by calling pg_reload_conf().
// Returns true if the reload was successful.
func ReloadConfig(ctx context.Context, pool *pgxpool.Pool) (bool, error) {
	var success bool
	err := pool.QueryRow(ctx, "SELECT pg_reload_conf()").Scan(&success)
	if err != nil {
		return false, err
	}
	return success, nil
}

// GetAllParametersCollect is a convenience wrapper using pgx.CollectRows.
func GetAllParametersCollect(ctx context.Context, pool *pgxpool.Pool) (*models.ConfigData, error) {
	const query = `
SELECT
    name,
    setting,
    COALESCE(unit, '') AS unit,
    category,
    short_desc,
    COALESCE(extra_desc, '') AS extra_desc,
    context,
    vartype,
    source,
    COALESCE(min_val, '') AS min_val,
    COALESCE(max_val, '') AS max_val,
    enumvals,
    COALESCE(boot_val, '') AS boot_val,
    COALESCE(reset_val, '') AS reset_val,
    COALESCE(sourcefile, '') AS sourcefile,
    COALESCE(sourceline, 0) AS sourceline,
    COALESCE(pending_restart, false) AS pending_restart
FROM pg_settings
ORDER BY name`

	rows, err := pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}

	params, err := pgx.CollectRows(rows, pgx.RowToStructByPos[models.Parameter])
	if err != nil {
		return nil, err
	}

	data := models.NewConfigData()
	data.Parameters = params
	categorySet := make(map[string]struct{})

	for _, p := range params {
		if p.IsModified() {
			data.ModifiedCount++
		}
		if p.PendingRestart {
			data.PendingRestartCount++
		}

		topCat := p.TopLevelCategory()
		if _, exists := categorySet[topCat]; !exists {
			categorySet[topCat] = struct{}{}
			data.Categories = append(data.Categories, topCat)
		}
	}

	return data, nil
}
