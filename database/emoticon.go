package database

import (
	"database/sql"
	"errors"

	"go.mau.fi/util/dbutil"
	log "maunium.net/go/maulogger/v2"
	"maunium.net/go/mautrix/id"
)

type EmoticonQuery struct {
	db  *Database
	log log.Logger
}

const (
	emoticonSelect = "SELECT mxid, mxc, mxalt, dcid, dcname FROM emoticon"
)

func (eq *EmoticonQuery) New() *Emoticon {
	return &Emoticon{
		db:  eq.db,
		log: eq.log,
	}
}

func (rq *EmoticonQuery) GetByMXIDAndMXC(mxid id.UserID, mxc string) *Emoticon {
	query := emoticonSelect + " WHERE mxid = $1 AND mxc=$2"

	return rq.get(query, mxid, mxc)
}

func (rq *EmoticonQuery) GetByMXC(mxc string) *Emoticon {
	query := emoticonSelect + " WHERE mxc=$1"

	return rq.get(query, mxc)
}

func (rq *EmoticonQuery) GetByAlt(alt string) *Emoticon {
	query := emoticonSelect + " WHERE mxalt=$1"

	return rq.get(query, alt)
}

func (rq *EmoticonQuery) get(query string, args ...interface{}) *Emoticon {
	row := rq.db.QueryRow(query, args...)
	if row == nil {
		return nil
	}

	return rq.New().Scan(row)
}

type Emoticon struct {
	db  *Database
	log log.Logger

	MXID   id.UserID
	MXC    string
	MXAlt  string
	DCName string
	DCID   string
}

func (e *Emoticon) Scan(row dbutil.Scannable) *Emoticon {
	err := row.Scan(&e.MXID, &e.MXC, &e.MXAlt, &e.DCID, &e.DCName)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			e.log.Errorln("Database scan failed:", err)
			panic(err)
		}
		return nil
	}

	return e
}

func (r *Emoticon) Insert() {
	query := `
		INSERT INTO emoticon (mxid, mxc, mxalt, dcid, dcname)
		VALUES($1, $2, $3, $4, $5)
	`
	_, err := r.db.Exec(query, r.MXID, r.MXC, r.MXAlt, r.DCID, r.DCName)
	if err != nil {
		r.log.Warnfln("Failed to insert emoticon for %s@%s: %v", r.MXC, r.MXID, err)
		panic(err)
	}
}

func (r *Emoticon) Delete() {
	query := "DELETE FROM emoticon WHERE mxid=$1 AND mxc=$2"
	_, err := r.db.Exec(query, r.MXID, r.MXC)
	if err != nil {
		r.log.Warnfln("Failed to delete reaction for %s@%s: %v", r.MXC, r.MXID, err)
		panic(err)
	}
}
