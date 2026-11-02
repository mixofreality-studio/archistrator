// Package todolist keeps each member's ordered todo list in Postgres behind the TodoListAccess contract.
package todolist

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	fwpostgres "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-postgres"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// Operation labels saved with each idempotency record.
const (
	opNew         = "RecordNewTodo"
	opReword      = "RecordRewording"
	opComplete    = "RecordCompletion"
	opCompleteAll = "RecordCompletionForAll"
	opDiscard     = "DiscardTodo"
	opDiscardDone = "DiscardCompletedTodos"
)

// Schema is private to this component and applied idempotently at construction.
// Member, todo id, key and title are bytea so every Go string round-trips byte for
// byte. pos is assigned once at record time and never altered: it is the list order.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS todolist_todos (
	member    bytea   NOT NULL,
	id        bytea   NOT NULL,
	pos       bigint GENERATED ALWAYS AS IDENTITY,
	title     bytea   NOT NULL,
	completed boolean NOT NULL DEFAULT false,
	PRIMARY KEY (member, id)
);
CREATE INDEX IF NOT EXISTS todolist_todos_member_pos ON todolist_todos (member, pos);
CREATE TABLE IF NOT EXISTS todolist_keys (
	member  bytea NOT NULL,
	key     bytea NOT NULL,
	op      text  NOT NULL,
	found   boolean NOT NULL DEFAULT false,
	todo_id bytea,
	removed bigint NOT NULL DEFAULT 0,
	PRIMARY KEY (member, key)
);`

type postgresTodoListAccess struct {
	pool *pgxpool.Pool
}

func newPostgresTodoListAccess(ctx context.Context, pool *pgxpool.Pool) (TodoListAccess, error) {
	if pool == nil {
		return nil, fwra.New(fwra.ContractMisuse, "todolist: nil pool")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fwpostgres.MapError(err, "todolist.New")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Serialize concurrent schema setup across processes.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('todolist_schema', 0))`); err != nil {
		return nil, fwpostgres.MapError(err, "todolist.New")
	}
	if _, err := tx.Exec(ctx, schemaSQL); err != nil {
		return nil, fwpostgres.MapError(err, "todolist.New")
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fwpostgres.MapError(err, "todolist.New")
	}
	return &postgresTodoListAccess{pool: pool}, nil
}

// outcome is the saved result of one logical write.
type outcome struct {
	found   bool
	todoID  []byte
	removed int64
}

// write runs one logical change as a single transaction with at-most-once effect
// per (member, key). apply runs only for the first call with a key.
func (a *postgresTodoListAccess) write(rc fwra.Context, member MemberID, op string, newID []byte,
	apply func(tx pgx.Tx, out *outcome) error) (outcome, error) {
	var out outcome
	if member == "" {
		return out, fwra.New(fwra.ContractMisuse, "todolist."+op+": empty member")
	}
	if rc.IdempotencyKey.IsZero() {
		return out, fwra.New(fwra.ContractMisuse, "todolist."+op+": missing idempotency key")
	}
	ctx := rc.Context
	if ctx == nil {
		ctx = context.Background()
	}
	label := "todolist." + op
	m, key := []byte(member), []byte(rc.IdempotencyKey)

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return out, fwpostgres.MapError(err, label)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// One writer per member at a time: simultaneous calls with a key apply once,
	// and bulk changes cover exactly the todos present when they commit.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(encode($1, 'hex'), 0))`, m); err != nil {
		return out, fwpostgres.MapError(err, label)
	}
	tag, err := tx.Exec(ctx,
		`INSERT INTO todolist_keys (member, key, op) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, m, key, op)
	if err != nil {
		return out, fwpostgres.MapError(err, label)
	}
	if tag.RowsAffected() == 0 {
		var savedOp string
		err := tx.QueryRow(ctx,
			`SELECT op, found, todo_id, removed FROM todolist_keys WHERE member = $1 AND key = $2`, m, key).
			Scan(&savedOp, &out.found, &out.todoID, &out.removed)
		if err != nil {
			return outcome{}, fwpostgres.MapError(err, label)
		}
		if savedOp != op {
			return outcome{}, fwra.New(fwra.ContractMisuse, label+": idempotency key reused for a different operation")
		}
		return out, nil
	}

	out.todoID = newID
	if err := apply(tx, &out); err != nil {
		return outcome{}, fwpostgres.MapError(err, label)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE todolist_keys SET found = $3, todo_id = $4, removed = $5 WHERE member = $1 AND key = $2`,
		m, key, out.found, out.todoID, out.removed); err != nil {
		return outcome{}, fwpostgres.MapError(err, label)
	}
	if err := tx.Commit(ctx); err != nil {
		return outcome{}, fwpostgres.MapError(err, label)
	}
	return out, nil
}

func misuse(op, what string) error {
	return fwra.New(fwra.ContractMisuse, "todolist."+op+": "+what)
}

func newTodoID() ([]byte, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, fwra.Wrap(fwra.Infrastructure, err, "todolist.RecordNewTodo: id generation failed")
	}
	return []byte(hex.EncodeToString(b[:])), nil
}

func (a *postgresTodoListAccess) RecordNewTodo(rc fwra.Context, member MemberID, title string) (TodoID, error) {
	if member == "" {
		return "", misuse(opNew, "empty member")
	}
	id, err := newTodoID()
	if err != nil {
		return "", err
	}
	out, err := a.write(rc, member, opNew, id, func(tx pgx.Tx, _ *outcome) error {
		_, err := tx.Exec(rc.Context,
			`INSERT INTO todolist_todos (member, id, title) VALUES ($1, $2, $3)`, []byte(member), id, []byte(title))
		return err
	})
	if err != nil {
		return "", err
	}
	return TodoID(out.todoID), nil
}

func (a *postgresTodoListAccess) RecordRewording(rc fwra.Context, member MemberID, todoID TodoID, title string) (TodoChangeOutcome, error) {
	if member == "" {
		return TodoChangeOutcome{}, misuse(opReword, "empty member")
	}
	if todoID == "" {
		return TodoChangeOutcome{}, misuse(opReword, "empty todo id")
	}
	out, err := a.write(rc, member, opReword, nil, func(tx pgx.Tx, o *outcome) error {
		tag, err := tx.Exec(rc.Context,
			`UPDATE todolist_todos SET title = $3 WHERE member = $1 AND id = $2`, []byte(member), []byte(todoID), []byte(title))
		o.found = err == nil && tag.RowsAffected() > 0
		return err
	})
	return TodoChangeOutcome{Found: out.found}, err
}

func (a *postgresTodoListAccess) RecordCompletion(rc fwra.Context, member MemberID, todoID TodoID, completed bool) (TodoChangeOutcome, error) {
	if member == "" {
		return TodoChangeOutcome{}, misuse(opComplete, "empty member")
	}
	if todoID == "" {
		return TodoChangeOutcome{}, misuse(opComplete, "empty todo id")
	}
	out, err := a.write(rc, member, opComplete, nil, func(tx pgx.Tx, o *outcome) error {
		tag, err := tx.Exec(rc.Context,
			`UPDATE todolist_todos SET completed = $3 WHERE member = $1 AND id = $2`, []byte(member), []byte(todoID), completed)
		o.found = err == nil && tag.RowsAffected() > 0
		return err
	})
	return TodoChangeOutcome{Found: out.found}, err
}

func (a *postgresTodoListAccess) RecordCompletionForAll(rc fwra.Context, member MemberID, completed bool) error {
	_, err := a.write(rc, member, opCompleteAll, nil, func(tx pgx.Tx, _ *outcome) error {
		_, err := tx.Exec(rc.Context,
			`UPDATE todolist_todos SET completed = $2 WHERE member = $1`, []byte(member), completed)
		return err
	})
	return err
}

func (a *postgresTodoListAccess) DiscardTodo(rc fwra.Context, member MemberID, todoID TodoID) (TodoChangeOutcome, error) {
	if member == "" {
		return TodoChangeOutcome{}, misuse(opDiscard, "empty member")
	}
	if todoID == "" {
		return TodoChangeOutcome{}, misuse(opDiscard, "empty todo id")
	}
	out, err := a.write(rc, member, opDiscard, nil, func(tx pgx.Tx, o *outcome) error {
		tag, err := tx.Exec(rc.Context,
			`DELETE FROM todolist_todos WHERE member = $1 AND id = $2`, []byte(member), []byte(todoID))
		o.found = err == nil && tag.RowsAffected() > 0
		return err
	})
	return TodoChangeOutcome{Found: out.found}, err
}

func (a *postgresTodoListAccess) DiscardCompletedTodos(rc fwra.Context, member MemberID) (int64, error) {
	out, err := a.write(rc, member, opDiscardDone, nil, func(tx pgx.Tx, o *outcome) error {
		tag, err := tx.Exec(rc.Context,
			`DELETE FROM todolist_todos WHERE member = $1 AND completed`, []byte(member))
		if err == nil {
			o.removed = tag.RowsAffected()
		}
		return err
	})
	return out.removed, err
}

func (a *postgresTodoListAccess) RecallTodoList(rc fwra.Context, member MemberID, filter TodoFilter) ([]Todo, error) {
	const op = "RecallTodoList"
	if member == "" {
		return nil, misuse(op, "empty member")
	}
	var where string
	switch filter {
	case TodoFilterAll:
		where = ""
	case TodoFilterActive:
		where = " AND NOT completed"
	case TodoFilterCompleted:
		where = " AND completed"
	default:
		return nil, misuse(op, "filter outside the enum")
	}
	ctx := rc.Context
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := a.pool.Query(ctx,
		`SELECT id, title, completed FROM todolist_todos WHERE member = $1`+where+` ORDER BY pos`, []byte(member))
	if err != nil {
		return nil, fwpostgres.MapError(err, "todolist."+op)
	}
	defer rows.Close()
	todos := []Todo{}
	for rows.Next() {
		var id, title []byte
		var done bool
		if err := rows.Scan(&id, &title, &done); err != nil {
			return nil, fwpostgres.MapError(err, "todolist."+op)
		}
		todos = append(todos, Todo{ID: TodoID(id), Title: string(title), Completed: done})
	}
	if err := rows.Err(); err != nil {
		return nil, fwpostgres.MapError(err, "todolist."+op)
	}
	return todos, nil
}
