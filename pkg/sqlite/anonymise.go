package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"math/big"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/doug-martin/goqu/v9"
	"github.com/doug-martin/goqu/v9/exp"
	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/logger"
	"github.com/stashapp/stash/pkg/txn"
	"github.com/stashapp/stash/pkg/utils"
)

const (
	letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	hex     = "0123456789abcdef"
)

type Anonymiser struct {
	*Database
}

func NewAnonymiser(db *Database, outPath string) (*Anonymiser, error) {
	if _, err := db.writeDB.Exec(fmt.Sprintf(`VACUUM INTO "%s"`, outPath)); err != nil {
		return nil, fmt.Errorf("vacuuming into %s: %w", outPath, err)
	}

	newDB := NewDatabase()
	if err := newDB.Open(outPath); err != nil {
		return nil, fmt.Errorf("opening %s: %w", outPath, err)
	}

	return &Anonymiser{Database: newDB}, nil
}

func (db *Anonymiser) Anonymise(ctx context.Context) error {
	if err := func() error {
		defer db.Close()

		return utils.Do([]func() error{
			func() error { return db.deleteBlobs() },
			func() error { return db.deleteStashIDs() },
			func() error { return db.clearOHistory() },
			func() error { return db.clearWatchHistory() },
			func() error { return db.anonymiseFolders(ctx) },
			func() error { return db.anonymiseFiles(ctx) },
			func() error { return db.anonymiseCaptions(ctx) },
			func() error { return db.anonymiseFingerprints(ctx) },
			func() error { return db.anonymiseScenes(ctx) },
			func() error { return db.anonymiseMarkers(ctx) },
			func() error { return db.anonymiseImages(ctx) },
			func() error { return db.anonymiseGalleries(ctx) },
			func() error { return db.anonymisePerformers(ctx) },
			func() error { return db.anonymiseStudios(ctx) },
			func() error { return db.anonymiseTags(ctx) },
			func() error { return db.anonymiseGroups(ctx) },
			func() error { return db.anonymiseSavedFilters(ctx) },
			func() error { return db.anonymiseNativeFilterEvidence(ctx) },
			func() error { return db.deleteSourceAccountEvidence(ctx) },
			func() error { return db.anonymiseArchiveUUIDs(ctx) },
			func() error { return db.Optimise(ctx) },
		})
	}(); err != nil {
		// delete the database
		_ = db.Remove()

		return err
	}

	return nil
}

func (db *Anonymiser) truncateColumn(tableName string, column string) error {
	_, err := db.writeDB.Exec("UPDATE " + tableName + " SET " + column + " = NULL")
	return err
}

func (db *Anonymiser) truncateTable(tableName string) error {
	_, err := db.writeDB.Exec("DELETE FROM " + tableName)
	return err
}

func (db *Anonymiser) deleteBlobs() error {
	return utils.Do([]func() error{
		func() error { return db.truncateColumn(tagTable, tagImageBlobColumn) },
		func() error { return db.truncateColumn(studioTable, studioImageBlobColumn) },
		func() error { return db.truncateColumn(performerTable, performerImageBlobColumn) },
		func() error { return db.truncateColumn(sceneTable, sceneCoverBlobColumn) },
		func() error { return db.truncateColumn(groupTable, groupFrontImageBlobColumn) },
		func() error { return db.truncateColumn(groupTable, groupBackImageBlobColumn) },

		func() error { return db.truncateTable(blobTable) },
	})
}

func (db *Anonymiser) deleteStashIDs() error {
	return utils.Do([]func() error{
		func() error { return db.truncateTable("scene_stash_ids") },
		func() error { return db.truncateTable("studio_stash_ids") },
		func() error { return db.truncateTable("performer_stash_ids") },
		func() error { return db.truncateTable("tag_stash_ids") },
	})
}

func (db *Anonymiser) clearOHistory() error {
	return utils.Do([]func() error{
		func() error { return db.truncateTable(scenesODatesTable) },
	})
}

func (db *Anonymiser) clearWatchHistory() error {
	return utils.Do([]func() error{
		func() error { return db.truncateTable(scenesViewDatesTable) },
	})
}

func (db *Anonymiser) anonymiseFolders(ctx context.Context) error {
	logger.Infof("Anonymising folders")
	return txn.WithTxn(ctx, db, func(ctx context.Context) error {
		return db.anonymiseFoldersRecurse(ctx, 0, "")
	})
}

func (db *Anonymiser) anonymiseFoldersRecurse(ctx context.Context, parentFolderID int, parentPath string) error {
	table := folderTableMgr.table

	stmt := dialect.Update(table)

	if parentFolderID == 0 {
		stmt = stmt.Set(goqu.Record{"path": goqu.Cast(table.Col(idColumn), "VARCHAR")}).Where(table.Col("parent_folder_id").IsNull())
	} else {
		stmt = stmt.Prepared(true).Set(goqu.Record{
			"path": goqu.L("? || ? || id", parentPath, string(filepath.Separator)),
		}).Where(table.Col("parent_folder_id").Eq(parentFolderID))
	}

	if _, err := exec(ctx, stmt); err != nil {
		return fmt.Errorf("anonymising %s: %w", table.GetTable(), err)
	}

	// now recurse to sub-folders
	query := dialect.From(table).Select(table.Col(idColumn), table.Col("path"))
	if parentFolderID == 0 {
		query = query.Where(table.Col("parent_folder_id").IsNull())
	} else {
		query = query.Where(table.Col("parent_folder_id").Eq(parentFolderID))
	}

	const single = false
	return queryFunc(ctx, query, single, func(rows *sqlx.Rows) error {
		var id int
		var path string
		if err := rows.Scan(&id, &path); err != nil {
			return err
		}

		return db.anonymiseFoldersRecurse(ctx, id, path)
	})
}

func (db *Anonymiser) anonymiseFiles(ctx context.Context) error {
	logger.Infof("Anonymising files")
	return txn.WithTxn(ctx, db, func(ctx context.Context) error {
		table := fileTableMgr.table
		stmt := dialect.Update(table).Set(goqu.Record{"basename": goqu.Cast(table.Col(idColumn), "VARCHAR")})

		if _, err := exec(ctx, stmt); err != nil {
			return fmt.Errorf("anonymising %s: %w", table.GetTable(), err)
		}

		return nil
	})
}

func (db *Anonymiser) anonymiseCaptions(ctx context.Context) error {
	logger.Infof("Anonymising captions")
	return txn.WithTxn(ctx, db, func(ctx context.Context) error {
		table := goqu.T(videoCaptionsTable)
		stmt := dialect.Update(table).Set(goqu.Record{"filename": goqu.Cast(table.Col("file_id"), "VARCHAR")})

		if _, err := exec(ctx, stmt); err != nil {
			return fmt.Errorf("anonymising %s: %w", table.GetTable(), err)
		}

		return nil
	})
}

func (db *Anonymiser) anonymiseFingerprints(ctx context.Context) error {
	logger.Infof("Anonymising fingerprints")
	table := fingerprintTableMgr.table
	lastID := 0
	lastType := ""
	total := 0
	const logEvery = 10000

	for gotSome := true; gotSome; {
		if err := txn.WithTxn(ctx, db, func(ctx context.Context) error {
			query := dialect.From(table).Select(
				table.Col(fileIDColumn),
				table.Col("type"),
				table.Col("fingerprint"),
			).Where(goqu.L("(file_id, type)").Gt(goqu.L("(?, ?)", lastID, lastType))).Limit(1000)

			gotSome = false

			const single = false
			return queryFunc(ctx, query, single, func(rows *sqlx.Rows) error {
				var (
					id          int
					typ         string
					fingerprint string
				)

				if err := rows.Scan(
					&id,
					&typ,
					&fingerprint,
				); err != nil {
					return err
				}

				if err := db.anonymiseFingerprint(ctx, table, "fingerprint", fingerprint); err != nil {
					return err
				}

				lastID = id
				lastType = typ

				gotSome = true
				total++

				if total%logEvery == 0 {
					logger.Infof("Anonymised %d fingerprints", total)
				}

				return nil
			})
		}); err != nil {
			return err
		}
	}

	return nil
}

func (db *Anonymiser) anonymiseScenes(ctx context.Context) error {
	logger.Infof("Anonymising scenes")
	table := sceneTableMgr.table
	lastID := 0
	total := 0
	const logEvery = 10000

	for gotSome := true; gotSome; {
		if err := txn.WithTxn(ctx, db, func(ctx context.Context) error {
			query := dialect.From(table).Select(
				table.Col(idColumn),
				table.Col("title"),
				table.Col("details"),
				table.Col("code"),
				table.Col("director"),
			).Where(table.Col(idColumn).Gt(lastID)).Limit(1000)

			gotSome = false

			const single = false
			return queryFunc(ctx, query, single, func(rows *sqlx.Rows) error {
				var (
					id       int
					title    sql.NullString
					details  sql.NullString
					code     sql.NullString
					director sql.NullString
				)

				if err := rows.Scan(
					&id,
					&title,
					&details,
					&code,
					&director,
				); err != nil {
					return err
				}

				set := goqu.Record{}

				// if title set set new title
				db.obfuscateNullString(set, "title", title)
				db.obfuscateNullString(set, "details", details)

				if len(set) > 0 {
					stmt := dialect.Update(table).Set(set).Where(table.Col(idColumn).Eq(id))

					if _, err := exec(ctx, stmt); err != nil {
						return fmt.Errorf("anonymising %s: %w", table.GetTable(), err)
					}
				}

				if code.Valid {
					if err := db.anonymiseText(ctx, table, "code", code.String); err != nil {
						return err
					}
				}

				if director.Valid {
					if err := db.anonymiseText(ctx, table, "director", director.String); err != nil {
						return err
					}
				}

				lastID = id
				gotSome = true
				total++

				if total%logEvery == 0 {
					logger.Infof("Anonymised %d scenes", total)
				}

				return nil
			})
		}); err != nil {
			return err
		}
	}

	if err := db.anonymiseURLs(ctx, goqu.T(scenesURLsTable), "scene_id"); err != nil {
		return err
	}

	if err := db.anonymiseCustomFields(ctx, goqu.T(scenesCustomFieldsTable.GetTable()), "scene_id"); err != nil {
		return err
	}

	return nil
}

func (db *Anonymiser) anonymiseMarkers(ctx context.Context) error {
	logger.Infof("Anonymising scene markers")
	table := sceneMarkerTableMgr.table
	lastID := 0
	total := 0
	const logEvery = 10000

	for gotSome := true; gotSome; {
		if err := txn.WithTxn(ctx, db, func(ctx context.Context) error {
			query := dialect.From(table).Select(
				table.Col(idColumn),
				table.Col("title"),
			).Where(table.Col(idColumn).Gt(lastID)).Limit(1000)

			gotSome = false

			const single = false
			return queryFunc(ctx, query, single, func(rows *sqlx.Rows) error {
				var (
					id    int
					title string
				)

				if err := rows.Scan(
					&id,
					&title,
				); err != nil {
					return err
				}

				if err := db.anonymiseText(ctx, table, "title", title); err != nil {
					return err
				}

				lastID = id
				gotSome = true
				total++

				if total%logEvery == 0 {
					logger.Infof("Anonymised %d scene markers", total)
				}

				return nil
			})
		}); err != nil {
			return err
		}
	}

	return nil
}

func (db *Anonymiser) anonymiseImages(ctx context.Context) error {
	logger.Infof("Anonymising images")
	table := imageTableMgr.table
	lastID := 0
	total := 0
	const logEvery = 10000

	for gotSome := true; gotSome; {
		if err := txn.WithTxn(ctx, db, func(ctx context.Context) error {
			query := dialect.From(table).Select(
				table.Col(idColumn),
				table.Col("title"),
			).Where(table.Col(idColumn).Gt(lastID)).Limit(1000)

			gotSome = false

			const single = false
			return queryFunc(ctx, query, single, func(rows *sqlx.Rows) error {
				var (
					id    int
					title sql.NullString
				)

				if err := rows.Scan(
					&id,
					&title,
				); err != nil {
					return err
				}

				set := goqu.Record{}
				db.obfuscateNullString(set, "title", title)

				if len(set) > 0 {
					stmt := dialect.Update(table).Set(set).Where(table.Col(idColumn).Eq(id))

					if _, err := exec(ctx, stmt); err != nil {
						return fmt.Errorf("anonymising %s: %w", table.GetTable(), err)
					}
				}

				lastID = id
				gotSome = true
				total++

				if total%logEvery == 0 {
					logger.Infof("Anonymised %d images", total)
				}

				return nil
			})
		}); err != nil {
			return err
		}
	}

	if err := db.anonymiseURLs(ctx, goqu.T(imagesURLsTable), "image_id"); err != nil {
		return err
	}

	return nil
}

func (db *Anonymiser) anonymiseGalleries(ctx context.Context) error {
	logger.Infof("Anonymising galleries")
	table := galleryTableMgr.table
	lastID := 0
	total := 0
	const logEvery = 10000

	for gotSome := true; gotSome; {
		if err := txn.WithTxn(ctx, db, func(ctx context.Context) error {
			query := dialect.From(table).Select(
				table.Col(idColumn),
				table.Col("title"),
				table.Col("details"),
				table.Col("photographer"),
			).Where(table.Col(idColumn).Gt(lastID)).Limit(1000)

			gotSome = false

			const single = false
			return queryFunc(ctx, query, single, func(rows *sqlx.Rows) error {
				var (
					id           int
					title        sql.NullString
					details      sql.NullString
					photographer sql.NullString
				)

				if err := rows.Scan(
					&id,
					&title,
					&details,
					&photographer,
				); err != nil {
					return err
				}

				set := goqu.Record{}
				db.obfuscateNullString(set, "title", title)
				db.obfuscateNullString(set, "details", details)
				db.obfuscateNullString(set, "photographer", photographer)

				if len(set) > 0 {
					stmt := dialect.Update(table).Set(set).Where(table.Col(idColumn).Eq(id))

					if _, err := exec(ctx, stmt); err != nil {
						return fmt.Errorf("anonymising %s: %w", table.GetTable(), err)
					}
				}

				lastID = id
				gotSome = true
				total++

				if total%logEvery == 0 {
					logger.Infof("Anonymised %d galleries", total)
				}

				return nil
			})
		}); err != nil {
			return err
		}
	}

	if err := db.anonymiseURLs(ctx, goqu.T(galleriesURLsTable), "gallery_id"); err != nil {
		return err
	}

	if err := db.anonymiseCustomFields(ctx, goqu.T(galleriesCustomFieldsTable.GetTable()), "gallery_id"); err != nil {
		return err
	}

	return nil
}

func (db *Anonymiser) anonymisePerformers(ctx context.Context) error {
	logger.Infof("Anonymising performers")
	table := performerTableMgr.table
	lastID := 0
	total := 0
	const logEvery = 10000

	for gotSome := true; gotSome; {
		if err := txn.WithTxn(ctx, db, func(ctx context.Context) error {
			query := dialect.From(table).Select(
				table.Col(idColumn),
				table.Col("disambiguation"),
				table.Col("details"),
				table.Col("tattoos"),
				table.Col("piercings"),
			).Where(table.Col(idColumn).Gt(lastID)).Limit(1000)

			gotSome = false

			const single = false
			return queryFunc(ctx, query, single, func(rows *sqlx.Rows) error {
				var (
					id             int
					disambiguation sql.NullString
					details        sql.NullString
					tattoos        sql.NullString
					piercings      sql.NullString
				)

				if err := rows.Scan(
					&id,
					&disambiguation,
					&details,
					&tattoos,
					&piercings,
				); err != nil {
					return err
				}

				set := goqu.Record{}
				db.obfuscateNullString(set, "disambiguation", disambiguation)
				db.obfuscateNullString(set, "details", details)
				db.obfuscateNullString(set, "tattoos", tattoos)
				db.obfuscateNullString(set, "piercings", piercings)

				if len(set) > 0 {
					stmt := dialect.Update(table).Set(set).Where(table.Col(idColumn).Eq(id))

					if _, err := exec(ctx, stmt); err != nil {
						return fmt.Errorf("anonymising %s: %w", table.GetTable(), err)
					}
				}

				lastID = id
				gotSome = true
				total++

				if total%logEvery == 0 {
					logger.Infof("Anonymised %d performers", total)
				}

				return nil
			})
		}); err != nil {
			return err
		}
	}

	if err := db.anonymisePerformerNames(ctx); err != nil {
		return err
	}

	if err := db.anonymiseURLs(ctx, goqu.T(performerURLsTable), "performer_id"); err != nil {
		return err
	}

	if err := db.anonymiseCustomFields(ctx, goqu.T(performersCustomFieldsTable.GetTable()), "performer_id"); err != nil {
		return err
	}

	return nil
}

func (db *Anonymiser) anonymiseStudios(ctx context.Context) error {
	logger.Infof("Anonymising studios")
	table := studioTableMgr.table
	lastID := 0
	total := 0
	const logEvery = 10000

	for gotSome := true; gotSome; {
		if err := txn.WithTxn(ctx, db, func(ctx context.Context) error {
			query := dialect.From(table).Select(
				table.Col(idColumn),
				table.Col("name"),
				table.Col("details"),
			).Where(table.Col(idColumn).Gt(lastID)).Limit(1000)

			gotSome = false

			const single = false
			return queryFunc(ctx, query, single, func(rows *sqlx.Rows) error {
				var (
					id      int
					name    sql.NullString
					details sql.NullString
				)

				if err := rows.Scan(
					&id,
					&name,
					&details,
				); err != nil {
					return err
				}

				set := goqu.Record{}
				db.obfuscateNullString(set, "name", name)
				db.obfuscateNullString(set, "details", details)

				if len(set) > 0 {
					stmt := dialect.Update(table).Set(set).Where(table.Col(idColumn).Eq(id))

					if _, err := exec(ctx, stmt); err != nil {
						return fmt.Errorf("anonymising %s: %w", table.GetTable(), err)
					}
				}

				lastID = id
				gotSome = true
				total++

				// TODO - anonymise studio aliases

				if total%logEvery == 0 {
					logger.Infof("Anonymised %d studios", total)
				}

				return nil
			})
		}); err != nil {
			return err
		}
	}

	if err := db.anonymiseAliases(ctx, goqu.T(studioAliasesTable), "studio_id"); err != nil {
		return err
	}

	if err := db.anonymiseURLs(ctx, goqu.T(studioURLsTable), "studio_id"); err != nil {
		return err
	}

	if err := db.anonymiseCustomFields(ctx, goqu.T(studiosCustomFieldsTable.GetTable()), "studio_id"); err != nil {
		return err
	}

	return nil
}

func (db *Anonymiser) anonymisePerformerNames(ctx context.Context) error {
	return txn.WithTxn(ctx, db, func(ctx context.Context) error {
		table := goqu.T(performerNamesTable)
		_, err := exec(ctx, dialect.Update(table).Set(goqu.Record{
			"name": goqu.L("'Performer ' || performer_id || ' name ' || position"),
		}))
		if err != nil {
			return fmt.Errorf("anonymising performer names: %w", err)
		}
		// The migration audit can retain old spellings that were deduplicated.
		_, err = exec(ctx, dialect.Update("native_migration_history").Set(goqu.Record{"details": "{}"}))
		return err
	})
}

func (db *Anonymiser) anonymiseAliases(ctx context.Context, table exp.IdentifierExpression, idColumn string) error {
	lastID := 0
	lastAlias := ""
	total := 0
	const logEvery = 10000

	for gotSome := true; gotSome; {
		if err := txn.WithTxn(ctx, db, func(ctx context.Context) error {
			query := dialect.From(table).Select(
				table.Col(idColumn),
				table.Col("alias"),
			).Where(goqu.L("(" + idColumn + ", alias)").Gt(goqu.L("(?, ?)", lastID, lastAlias))).Limit(1000)

			gotSome = false

			const single = false
			return queryFunc(ctx, query, single, func(rows *sqlx.Rows) error {
				var (
					id    int
					alias sql.NullString
				)

				if err := rows.Scan(
					&id,
					&alias,
				); err != nil {
					return err
				}

				set := goqu.Record{}
				db.obfuscateNullString(set, "alias", alias)

				if len(set) > 0 {
					stmt := dialect.Update(table).Set(set).Where(
						table.Col(idColumn).Eq(id),
						table.Col("alias").Eq(alias),
					)

					if _, err := exec(ctx, stmt); err != nil {
						return fmt.Errorf("anonymising %s: %w", table.GetTable(), err)
					}
				}

				lastID = id
				lastAlias = alias.String
				gotSome = true
				total++

				if total%logEvery == 0 {
					logger.Infof("Anonymised %d %s aliases", total, table.GetTable())
				}

				return nil
			})
		}); err != nil {
			return err
		}
	}

	return nil
}

func (db *Anonymiser) anonymiseURLs(ctx context.Context, table exp.IdentifierExpression, idColumn string) error {
	lastID := 0
	lastURL := ""
	total := 0
	const logEvery = 10000

	for gotSome := true; gotSome; {
		if err := txn.WithTxn(ctx, db, func(ctx context.Context) error {
			query := dialect.From(table).Select(
				table.Col(idColumn),
				table.Col("url"),
			).Where(goqu.L("(" + idColumn + ", url)").Gt(goqu.L("(?, ?)", lastID, lastURL))).Limit(1000)

			gotSome = false

			const single = false
			return queryFunc(ctx, query, single, func(rows *sqlx.Rows) error {
				var (
					id  int
					url sql.NullString
				)

				if err := rows.Scan(
					&id,
					&url,
				); err != nil {
					return err
				}

				set := goqu.Record{}
				db.obfuscateNullString(set, "url", url)

				if len(set) > 0 {
					stmt := dialect.Update(table).Set(set).Where(
						table.Col(idColumn).Eq(id),
						table.Col("url").Eq(url),
					)

					if _, err := exec(ctx, stmt); err != nil {
						return fmt.Errorf("anonymising %s: %w", table.GetTable(), err)
					}
				}

				lastID = id
				lastURL = url.String
				gotSome = true
				total++

				if total%logEvery == 0 {
					logger.Infof("Anonymised %d %s URLs", total, table.GetTable())
				}

				return nil
			})
		}); err != nil {
			return err
		}
	}

	return nil
}

func (db *Anonymiser) anonymiseTags(ctx context.Context) error {
	logger.Infof("Anonymising tags")
	table := tagTableMgr.table
	lastID := 0
	total := 0
	const logEvery = 10000

	for gotSome := true; gotSome; {
		if err := txn.WithTxn(ctx, db, func(ctx context.Context) error {
			query := dialect.From(table).Select(
				table.Col(idColumn),
				table.Col("name"),
				table.Col("sort_name"),
				table.Col("description"),
			).Where(table.Col(idColumn).Gt(lastID)).Limit(1000)

			gotSome = false

			const single = false
			return queryFunc(ctx, query, single, func(rows *sqlx.Rows) error {
				var (
					id          int
					name        sql.NullString
					sortName    sql.NullString
					description sql.NullString
				)

				if err := rows.Scan(
					&id,
					&name,
					&sortName,
					&description,
				); err != nil {
					return err
				}

				set := goqu.Record{}
				db.obfuscateNullString(set, "name", name)
				db.obfuscateNullString(set, "sort_name", sortName)
				db.obfuscateNullString(set, "description", description)

				if len(set) > 0 {
					stmt := dialect.Update(table).Set(set).Where(table.Col(idColumn).Eq(id))

					if _, err := exec(ctx, stmt); err != nil {
						return fmt.Errorf("anonymising %s: %w", table.GetTable(), err)
					}
				}

				lastID = id
				gotSome = true
				total++

				if total%logEvery == 0 {
					logger.Infof("Anonymised %d tags", total)
				}

				return nil
			})
		}); err != nil {
			return err
		}
	}

	if err := db.anonymiseAliases(ctx, goqu.T(tagAliasesTable), "tag_id"); err != nil {
		return err
	}

	if err := db.anonymiseCustomFields(ctx, goqu.T(tagsCustomFieldsTable.GetTable()), "tag_id"); err != nil {
		return err
	}

	return nil
}

func (db *Anonymiser) anonymiseGroups(ctx context.Context) error {
	logger.Infof("Anonymising groups")
	table := groupTableMgr.table
	lastID := 0
	total := 0
	const logEvery = 10000

	for gotSome := true; gotSome; {
		if err := txn.WithTxn(ctx, db, func(ctx context.Context) error {
			query := dialect.From(table).Select(
				table.Col(idColumn),
				table.Col("name"),
				table.Col("aliases"),
				table.Col("description"),
				table.Col("director"),
			).Where(table.Col(idColumn).Gt(lastID)).Limit(1000)

			gotSome = false

			const single = false
			return queryFunc(ctx, query, single, func(rows *sqlx.Rows) error {
				var (
					id          int
					name        sql.NullString
					aliases     sql.NullString
					description sql.NullString
					director    sql.NullString
				)

				if err := rows.Scan(
					&id,
					&name,
					&aliases,
					&description,
					&director,
				); err != nil {
					return err
				}

				set := goqu.Record{}
				db.obfuscateNullString(set, "name", name)
				db.obfuscateNullString(set, "aliases", aliases)
				db.obfuscateNullString(set, "description", description)
				db.obfuscateNullString(set, "director", director)

				if len(set) > 0 {
					stmt := dialect.Update(table).Set(set).Where(table.Col(idColumn).Eq(id))

					if _, err := exec(ctx, stmt); err != nil {
						return fmt.Errorf("anonymising %s: %w", table.GetTable(), err)
					}
				}

				lastID = id
				gotSome = true
				total++

				if total%logEvery == 0 {
					logger.Infof("Anonymised %d groups", total)
				}

				return nil
			})
		}); err != nil {
			return err
		}
	}

	if err := db.anonymiseURLs(ctx, goqu.T(groupURLsTable), "group_id"); err != nil {
		return err
	}

	if err := db.anonymiseCustomFields(ctx, goqu.T(groupsCustomFieldsTable.GetTable()), "group_id"); err != nil {
		return err
	}

	return nil
}

func (db *Anonymiser) anonymiseSavedFilters(ctx context.Context) error {
	logger.Infof("Anonymising saved filters")
	table := savedFilterTableMgr.table
	lastID := 0
	total := 0
	const logEvery = 10000

	for gotSome := true; gotSome; {
		if err := txn.WithTxn(ctx, db, func(ctx context.Context) error {
			query := dialect.From(table).Select(
				table.Col(idColumn),
				table.Col("name"),
			).Where(table.Col(idColumn).Gt(lastID)).Limit(1000)

			gotSome = false

			const single = false
			return queryFunc(ctx, query, single, func(rows *sqlx.Rows) error {
				var (
					id   int
					name sql.NullString
				)

				if err := rows.Scan(
					&id,
					&name,
				); err != nil {
					return err
				}

				set := goqu.Record{}
				db.obfuscateNullString(set, "name", name)

				if len(set) > 0 {
					stmt := dialect.Update(table).Set(set).Where(table.Col(idColumn).Eq(id))

					if _, err := exec(ctx, stmt); err != nil {
						return fmt.Errorf("anonymising %s: %w", table.GetTable(), err)
					}
				}

				lastID = id
				gotSome = true
				total++

				if total%logEvery == 0 {
					logger.Infof("Anonymised %d saved filters", total)
				}

				return nil
			})
		}); err != nil {
			return err
		}
	}

	return nil
}

func (db *Anonymiser) anonymiseText(ctx context.Context, table exp.IdentifierExpression, column string, value string) error {
	set := goqu.Record{}
	set[column] = db.obfuscateString(value, letters)

	stmt := dialect.Update(table).Set(set).Where(table.Col(column).Eq(value))

	if _, err := exec(ctx, stmt); err != nil {
		return fmt.Errorf("anonymising %s: %w", column, err)
	}

	return nil
}

func (db *Anonymiser) anonymiseNativeFilterEvidence(ctx context.Context) error {
	return txn.WithTxn(ctx, db, func(ctx context.Context) error {
		for _, query := range []string{
			"DELETE FROM saved_filter_import_conflicts",
			"DELETE FROM default_filter_import_conflicts",
			"UPDATE configuration_migrations SET source_json = '{}', target_json = '{}'",
			"UPDATE saved_filters SET find_filter = '', filter_ast = '', ui_options = ''",
			"UPDATE default_filters SET find_filter = '', filter_ast = '', ui_options = ''",
		} {
			if _, err := dbWrapper.Exec(ctx, query); err != nil {
				return err
			}
		}
		return nil
	})
}

func (db *Anonymiser) anonymiseArchiveUUIDs(ctx context.Context) error {
	return txn.WithTxn(ctx, db, func(ctx context.Context) error {
		// Retain the graph while preventing UUIDs from identifying the source
		// library. Foreign keys cascade through aliases and merge redirects.
		_, err := dbWrapper.Exec(ctx, "UPDATE archive_entities SET uuid = "+archiveUUIDExpression)
		return err
	})
}

func (db *Anonymiser) deleteSourceAccountEvidence(ctx context.Context) error {
	return txn.WithTxn(ctx, db, func(ctx context.Context) error {
		// Only this isolated export discards policy history. Restore the guard
		// in the same transaction; failure rolls back both schema and deletes.
		retainedGuards := []string{}
		for _, name := range []string{"source_run_policy_upgrade_retained", "metadata_worker_policy_retained", "metadata_worker_attempt_policy_retained"} {
			var definition string
			if err := dbWrapper.Get(ctx, &definition, "SELECT sql FROM sqlite_schema WHERE type='trigger' AND name=?", name); err != nil {
				return err
			}
			if _, err := dbWrapper.Exec(ctx, "DROP TRIGGER "+name); err != nil {
				return err
			}
			retainedGuards = append(retainedGuards, definition)
		}
		for _, table := range []string{
			"account_profile_urls", "performer_profile_url_suppressions",
			"provider_metadata_imports",
			"file_deduplications",
			"discovery_published_records", "discovery_match_publications",
			"discovery_detail_results", "discovery_detail_checkpoints", "discovery_detail_checkpoint_records", "discovery_detail_checkpoint_receipts", "discovery_detail_attempts", "discovery_detail_jobs",
			"discovery_activation_targets", "discovery_activations",
			"discovery_scope_reviews",
			"discovery_recovery_targets", "discovery_listing_recoveries",
			"discovery_match_evidence", "discovery_match_candidates", "discovery_match_pages", "discovery_match_targets",
			"discovery_pages", "discovery_job_attempts", "discovery_listing_jobs", "discovery_listing_legacy", "discovery_listings",
			"account_ownership_reviews",
			"post_consolidation_review_media", "post_consolidation_review_attachments", "post_consolidation_review_members", "post_consolidation_reviews",
			"gallery_association_reviews", "attachment_media_reviews",
			"attachment_selection_reviews",
			"enrichment_discovery_resolutions",
			"metadata_file_edit_keeps", "metadata_file_edit_reviews",
			"source_capture_contexts",
			"enrichment_job_seed_services", "enrichment_job_retained_records", "enrichment_handoff_jobs",
			"checkpoint_handoffs", "checkpoint_evidence_captures", "checkpoint_evidence_acceptances",
			"automation_checkpoint_records", "automation_checkpoint_imports", "automation_checkpoint_bodies",
			"enrichment_rebinding_targets", "enrichment_rebindings",
			"enrichment_activation_targets", "enrichment_activations",
			"automation_discovery_records", "automation_discovery_imports",
			"automation_enrichment_records", "automation_enrichment_imports",
			"file_path_fences",
			"source_attachment_downloads",
			"ingest_receipts",
			"scan_journal_activation_jobs", "scan_journal_activations",
			"scan_journal_records", "scan_journals",
			"catalog_publisher_records", "catalog_publisher_imports",
			"catalog_membership_records", "catalog_membership_imports", "catalog_membership_groups",
			"catalog_file_history_records", "catalog_file_history_imports",
			"source_file_history_locations", "source_file_history_edits", "source_file_history_states", "source_file_history_deduplications", "source_file_history",
			"catalog_media_records", "catalog_media_imports",
			"catalog_attachment_records", "catalog_attachment_imports",
			"catalog_evidence_records", "catalog_evidence_posts", "catalog_evidence_imports",
			"catalog_relation_records", "catalog_relations_imports",
			"catalog_document_records", "catalog_document_imports",
			"catalog_cleanup_records", "catalog_cleanup_imports", "source_cleanup_intents",
			"catalog_enrichment_records", "catalog_enrichment_imports",
			"catalog_translation_records", "catalog_translation_imports",
			"capture_translation_entries", "capture_translation_decisions", "translation_policy_revisions", "translation_policies",
			"translation_activation_targets", "translation_activations",
			"automation_translation_records", "automation_translation_imports",
			"catalog_snapshot_records", "catalog_snapshot_chunks", "catalog_snapshot_tables", "catalog_snapshots",
			"automation_snapshot_records", "automation_snapshot_chunks", "automation_snapshot_tables", "automation_snapshots",
			"catalog_account_mappings", "catalog_collection_mappings",
			"catalog_registry_import_records", "catalog_registry_imports",
			"catalog_identity_import_records", "catalog_identity_imports",
			"source_backfill_requests", "source_backfill_decisions",
			"source_enrichment_waiter_scopes", "source_enrichment_waiters", "source_service_turns",
			"source_run_attempt_failures", "source_run_attempt_pacing", "enrichment_attempt_pacing", "source_run_pacing", "enrichment_job_pacing", "source_pacing",
			"source_run_policy_upgrades", "source_run_requests", "source_run_attempts", "source_run_reviews", "source_runs", "source_run_cooldowns",
			"enrichment_checkpoint_releases", "enrichment_published_records", "enrichment_publications",
			"enrichment_checkpoints", "enrichment_checkpoint_records", "enrichment_checkpoint_receipts", "enrichment_job_attempts", "enrichment_job_targets",
			"metadata_worker_attempt_policies", "metadata_worker_policy_upgrades",
			"translation_job_targets", "archive_job_submissions", "archive_job_attempts", "archive_jobs",
			"file_content_versions", "media_contents",
			"post_media_decision_evidence", "post_media_backfill_decisions", "post_media_backfills",
			"source_post_file_evidence", "source_file_matches", "source_file_observations", "source_content_claims",
			"translation_target_history", "translation_targets", "translation_cache", "translation_requests",
			"enrichment_target_history", "enrichment_completion_captures", "enrichment_completions", "enrichment_targets",
			"source_enrichment_receipts",
			"source_translation_evidence", "source_translations",
			"metadata_policy_import_documents", "metadata_policy_imports",
			"source_document_heads", "source_document_head_decisions", "source_document_head_claims", "source_document_sources", "source_documents", "source_document_contents",
			"ingest_credential_scopes", "ingest_credential_roots", "ingest_credentials", "ingest_producers",
			"metadata_decision_post_media", "post_media_consolidation_edges", "post_media_links", "post_media_supersessions", "post_media_decisions",
			"metadata_decision_policies", "metadata_policy_revisions", "metadata_policies",
			"metadata_field_write_context", "metadata_field_pending", "metadata_field_heads", "metadata_field_references", "metadata_field_decisions",
			"source_gallery_write_context", "gallery_membership_heads", "gallery_membership_events", "post_gallery_links", "post_gallery_decisions",
			"post_attachment_selections", "post_attachment_decision_manifests", "post_attachment_decisions",
			"attachment_media_links", "attachment_media_decisions", "source_media_evidence", "source_capture_attachment_manifests", "source_attachment_entries", "source_attachment_manifests", "source_attachments",
			"capture_publisher_write_context", "capture_publisher_claims", "capture_publisher_heads", "capture_publisher_decisions",
			"source_collection_post_evidence", "source_collection_captures", "source_collection_media_intake", "source_collection_revisions", "source_collections", "media_root_revisions", "media_roots",
			"source_post_account_claims", "source_post_identifier_evidence", "source_post_url_evidence", "source_post_urls",
			"source_capture_sightings", "source_capture_content", "source_capture_profiles", "source_captures", "source_post_revisions", "source_post_identifiers",
			"source_post_consolidation_context", "source_post_consolidations", "source_post_identities", "source_posts", "source_profile_bodies", "source_payloads",
			"source_account_consolidation_context", "source_account_consolidations",
			"account_performer_links", "account_performer_decisions", "source_account_identifier_evidence", "source_account_identifiers", "source_accounts",
		} {
			if _, err := dbWrapper.Exec(ctx, "DELETE FROM "+table); err != nil {
				return err
			}
		}
		for _, definition := range retainedGuards {
			if _, err := dbWrapper.Exec(ctx, definition); err != nil {
				return err
			}
		}
		return nil
	})
}

func (db *Anonymiser) anonymiseFingerprint(ctx context.Context, table exp.IdentifierExpression, column string, value string) error {
	set := goqu.Record{}
	set[column] = db.obfuscateString(value, hex)

	stmt := dialect.Update(table).Set(set).Where(table.Col(column).Eq(value))

	if _, err := exec(ctx, stmt); err != nil {
		return fmt.Errorf("anonymising %s: %w", column, err)
	}

	return nil
}

func (db *Anonymiser) obfuscateNullString(out goqu.Record, column string, in sql.NullString) {
	if in.Valid {
		out[column] = db.obfuscateString(in.String, letters)
	}
}

func (db *Anonymiser) obfuscateString(in string, dict string) string {
	out := strings.Builder{}
	for _, c := range in {
		if unicode.IsSpace(c) {
			out.WriteRune(c)
		} else {
			num, err := rand.Int(rand.Reader, big.NewInt(int64(len(dict))))
			if err != nil {
				panic("error generating random number")
			}

			out.WriteByte(dict[num.Int64()])
		}
	}

	return out.String()
}

func (db *Anonymiser) anonymiseCustomFields(ctx context.Context, table exp.IdentifierExpression, idColumn string) error {
	lastID := 0
	lastField := ""
	total := 0
	const logEvery = 10000

	for gotSome := true; gotSome; {
		if err := txn.WithTxn(ctx, db, func(ctx context.Context) error {
			query := dialect.From(table).Select(
				table.Col(idColumn),
				table.Col("field"),
				table.Col("value"),
			).Where(
				goqu.L("("+idColumn+", field)").Gt(goqu.L("(?, ?)", lastID, lastField)),
			).Order(
				table.Col(idColumn).Asc(), table.Col("field").Asc(),
			).Limit(1000)

			gotSome = false

			const single = false
			return queryFunc(ctx, query, single, func(rows *sqlx.Rows) error {
				var (
					id    int
					field string
					value string
				)

				if err := rows.Scan(
					&id,
					&field,
					&value,
				); err != nil {
					return err
				}

				set := goqu.Record{}
				set["field"] = db.obfuscateString(field, letters)
				set["value"] = db.obfuscateString(value, letters)

				if len(set) > 0 {
					stmt := dialect.Update(table).Set(set).Where(
						table.Col(idColumn).Eq(id),
						table.Col("field").Eq(field),
					)

					if _, err := exec(ctx, stmt); err != nil {
						return fmt.Errorf("anonymising %s: %w", table.GetTable(), err)
					}
				}

				lastID = id
				lastField = field
				gotSome = true
				total++

				if total%logEvery == 0 {
					logger.Infof("Anonymised %d %s custom fields", total, table.GetTable())
				}

				return nil
			})
		}); err != nil {
			return err
		}
	}

	return nil
}
