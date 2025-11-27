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
