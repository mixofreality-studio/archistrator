// Package todolist is the TodoListAccess ResourceAccess component: the durable,
// per-member todo list kept in Postgres.
package todolist

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

const schemaDDL = `
CREATE TABLE IF NOT EXISTS todo_items (
	position  BIGSERIAL PRIMARY KEY,
	member    TEXT    NOT NULL,
	id        TEXT    NOT NULL,
	title     TEXT    NOT NULL,
	completed BOOLEAN NOT NULL DEFAULT FALSE,
	UNIQUE (member, id)
);
CREATE INDEX IF NOT EXISTS todo_items_member_idx ON todo_items (member, position);
`

type postgresTodoListAccess struct {
	pool *pgxpool.Pool
}

// newPostgresTodoListAccess creates the store's schema idempotently and returns the access.
func newPostgresTodoListAccess(ctx context.Context, pool *pgxpool.Pool) (TodoListAccess, error) {
	if pool == nil {
		return nil, fwra.New(fwra.ContractMisuse, "nil connection pool")
	}
	// Serialize concurrent start-ups so CREATE ... IF NOT EXISTS cannot race.
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, unavailable(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(7403117001)"); err != nil {
		return nil, unavailable(err)
	}
	if _, err := tx.Exec(ctx, schemaDDL); err != nil {
		return nil, unavailable(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, unavailable(err)
	}
	return &postgresTodoListAccess{pool: pool}, nil
}

func unavailable(err error) error {
	return fwra.Wrap(fwra.Infrastructure, err, "todo store unavailable")
}

func invalid(detail string) error { return fwra.New(fwra.ContractMisuse, detail) }

func notFound() error { return fwra.New(fwra.NotFound, "todo not found") }

func checkMember(m MemberID) error {
	if m == "" {
		return invalid("empty member")
	}
	return nil
}

func checkTodo(m MemberID, t TodoID) error {
	if err := checkMember(m); err != nil {
		return err
	}
	if t == "" {
		return invalid("empty todo id")
	}
	return nil
}

func (a *postgresTodoListAccess) RecordTodo(rc fwra.Context, member MemberID, title string) (Todo, error) {
	if err := checkMember(member); err != nil {
		return Todo{}, err
	}
	id := TodoID(uuid.NewString())
	if _, err := a.pool.Exec(rc,
		`INSERT INTO todo_items (member, id, title, completed) VALUES ($1, $2, $3, FALSE)`,
		string(member), string(id), title); err != nil {
		return Todo{}, unavailable(err)
	}
	return Todo{Id: id, Title: title, Completed: false}, nil
}

func (a *postgresTodoListAccess) RewordTodo(rc fwra.Context, member MemberID, todo TodoID, title string) error {
	if err := checkTodo(member, todo); err != nil {
		return err
	}
	tag, err := a.pool.Exec(rc,
		`UPDATE todo_items SET title = $3 WHERE member = $1 AND id = $2`,
		string(member), string(todo), title)
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() == 0 {
		return notFound()
	}
	return nil
}

func (a *postgresTodoListAccess) StrikeTodo(rc fwra.Context, member MemberID, todo TodoID) error {
	if err := checkTodo(member, todo); err != nil {
		return err
	}
	if _, err := a.pool.Exec(rc,
		`DELETE FROM todo_items WHERE member = $1 AND id = $2`,
		string(member), string(todo)); err != nil {
		return unavailable(err)
	}
	return nil
}

func (a *postgresTodoListAccess) MarkTodo(rc fwra.Context, member MemberID, todo TodoID, completed bool) error {
	if err := checkTodo(member, todo); err != nil {
		return err
	}
	tag, err := a.pool.Exec(rc,
		`UPDATE todo_items SET completed = $3 WHERE member = $1 AND id = $2`,
		string(member), string(todo), completed)
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() == 0 {
		return notFound()
	}
	return nil
}

func (a *postgresTodoListAccess) MarkEveryTodo(rc fwra.Context, member MemberID, completed bool) error {
	if err := checkMember(member); err != nil {
		return err
	}
	// A single statement is atomic: all or nothing.
	if _, err := a.pool.Exec(rc,
		`UPDATE todo_items SET completed = $2 WHERE member = $1`,
		string(member), completed); err != nil {
		return unavailable(err)
	}
	return nil
}

func (a *postgresTodoListAccess) PurgeCompleted(rc fwra.Context, member MemberID) error {
	if err := checkMember(member); err != nil {
		return err
	}
	if _, err := a.pool.Exec(rc,
		`DELETE FROM todo_items WHERE member = $1 AND completed`,
		string(member)); err != nil {
		return unavailable(err)
	}
	return nil
}

func (a *postgresTodoListAccess) RecallList(rc fwra.Context, member MemberID) (TodoList, error) {
	if err := checkMember(member); err != nil {
		return TodoList{}, err
	}
	return a.query(rc, `SELECT id, title, completed FROM todo_items WHERE member = $1 ORDER BY position`, string(member))
}

func (a *postgresTodoListAccess) SelectInView(rc fwra.Context, member MemberID, view View) (TodoList, error) {
	if err := checkMember(member); err != nil {
		return TodoList{}, err
	}
	switch view {
	case "all":
		return a.RecallList(rc, member)
	case "active":
		return a.query(rc, `SELECT id, title, completed FROM todo_items WHERE member = $1 AND NOT completed ORDER BY position`, string(member))
	case "completed":
		return a.query(rc, `SELECT id, title, completed FROM todo_items WHERE member = $1 AND completed ORDER BY position`, string(member))
	default:
		return TodoList{}, invalid("unknown view")
	}
}

func (a *postgresTodoListAccess) TallyItemsLeft(rc fwra.Context, member MemberID) (ItemsLeft, error) {
	if err := checkMember(member); err != nil {
		return ItemsLeft{}, err
	}
	var n int64
	err := a.pool.QueryRow(rc,
		`SELECT count(*) FROM todo_items WHERE member = $1 AND NOT completed`,
		string(member)).Scan(&n)
	if err != nil {
		return ItemsLeft{}, unavailable(err)
	}
	return ItemsLeft{Count: n}, nil
}

func (a *postgresTodoListAccess) query(ctx context.Context, sql string, args ...any) (TodoList, error) {
	rows, err := a.pool.Query(ctx, sql, args...)
	if err != nil {
		return TodoList{}, unavailable(err)
	}
	todos, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Todo, error) {
		var t Todo
		var id string
		if err := r.Scan(&id, &t.Title, &t.Completed); err != nil {
			return Todo{}, err
		}
		t.Id = TodoID(id)
		return t, nil
	})
	if err != nil {
		return TodoList{}, unavailable(err)
	}
	if todos == nil {
		todos = []Todo{}
	}
	return TodoList{Todos: todos}, nil
}
