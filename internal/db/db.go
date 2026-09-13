// Package db ouvre la base SQLite et applique les migrations embarquées.
package db

import (
	"database/sql"
	"embed"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite" // pilote SQLite pur Go : binaire autonome, sans cgo
)

//go:embed migrations/*.sql
var migrations embed.FS

// Ouvrir ouvre la base et applique les migrations en attente.
//
// Les pragmas sont posés dans le DSN plutôt qu'après connexion : le pool de
// database/sql peut ouvrir plusieurs connexions, et une pragma exécutée sur
// une seule d'entre elles ne s'appliquerait pas aux autres.
func Ouvrir(chemin string) (*sql.DB, error) {
	dsn := fmt.Sprintf(
		"file:%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"+
			"&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)",
		chemin)

	base, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("ouverture de %s : %w", chemin, err)
	}

	// SQLite n'accepte qu'un écrivain à la fois ; le busy_timeout gère
	// l'attente. Un pool large n'apporterait rien et multiplierait les
	// verrous : trois ou quatre rédacteurs simultanés sont largement couverts.
	base.SetMaxOpenConns(8)
	base.SetMaxIdleConns(4)

	if err := base.Ping(); err != nil {
		base.Close()
		return nil, fmt.Errorf("connexion à %s : %w", chemin, err)
	}
	if err := Migrer(base); err != nil {
		base.Close()
		return nil, err
	}
	return base, nil
}

// Migrer applique les migrations non encore appliquées, dans l'ordre des noms
// de fichiers. Chaque migration s'exécute dans sa propre transaction : une
// migration qui échoue ne laisse pas la base à moitié transformée.
func Migrer(base *sql.DB) error {
	if _, err := base.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applique_le TEXT NOT NULL DEFAULT (datetime('now'))
		)`); err != nil {
		return fmt.Errorf("création de schema_migrations : %w", err)
	}

	appliquees, err := versionsAppliquees(base)
	if err != nil {
		return err
	}

	fichiers, err := migrations.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("lecture des migrations embarquées : %w", err)
	}
	noms := make([]string, 0, len(fichiers))
	for _, f := range fichiers {
		if !f.IsDir() && strings.HasSuffix(f.Name(), ".sql") {
			noms = append(noms, f.Name())
		}
	}
	sort.Strings(noms)

	for _, nom := range noms {
		version := strings.TrimSuffix(nom, filepath.Ext(nom))
		if appliquees[version] {
			continue
		}
		contenu, err := migrations.ReadFile("migrations/" + nom)
		if err != nil {
			return fmt.Errorf("lecture de %s : %w", nom, err)
		}
		if err := appliquer(base, version, string(contenu)); err != nil {
			return err
		}
	}
	return nil
}

func versionsAppliquees(base *sql.DB) (map[string]bool, error) {
	lignes, err := base.Query("SELECT version FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("lecture de schema_migrations : %w", err)
	}
	defer lignes.Close()

	out := map[string]bool{}
	for lignes.Next() {
		var v string
		if err := lignes.Scan(&v); err != nil {
			return nil, err
		}
		out[v] = true
	}
	return out, lignes.Err()
}

func appliquer(base *sql.DB, version, contenu string) error {
	tx, err := base.Begin()
	if err != nil {
		return fmt.Errorf("migration %s : %w", version, err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(contenu); err != nil {
		return fmt.Errorf("migration %s : %w", version, err)
	}
	if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES (?)", version); err != nil {
		return fmt.Errorf("enregistrement de la migration %s : %w", version, err)
	}
	return tx.Commit()
}

// Sauvegarder produit une copie cohérente de la base dans un fichier, sans
// interrompre les lecteurs. Le fichier obtenu est directement déposable
// sur le stockage S3.
func Sauvegarder(base *sql.DB, destination string) error {
	if destination == "" {
		return fmt.Errorf("destination de sauvegarde vide")
	}

	// SQLite accepte un paramètre lié après VACUUM INTO, mais tous les
	// pilotes ne préparent pas cette instruction. On tente la forme liée,
	// puis on retombe sur un littéral échappé.
	if _, err := base.Exec("VACUUM INTO ?", destination); err == nil {
		return nil
	}

	litteral := "'" + strings.ReplaceAll(destination, "'", "''") + "'"
	if _, err := base.Exec("VACUUM INTO " + litteral); err != nil {
		return fmt.Errorf("sauvegarde vers %s : %w", destination, err)
	}
	return nil
}
