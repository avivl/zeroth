package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/avivl/zeroth/internal/store"
)

const planCols = `id, session_id, parent_plan_id, status, summary, effects_json, cross_exam_json, secret_scan_findings_json, review_comment, created_at_unix_nano, updated_at_unix_nano`

func (s *Store) CreatePlan(ctx context.Context, p store.Plan) error {
	if err := s.guard(); err != nil {
		return err
	}
	if p.ID.IsZero() || p.SessionID.IsZero() || p.Status == "" || p.Summary == "" {
		return wrap("create plan", store.ErrInvalid)
	}
	effects, exam, findings, err := planJSON(p)
	if err != nil {
		return wrap("create plan", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO plans (`+planCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID.String(), p.SessionID.String(), p.ParentPlanID.String(), p.Status, p.Summary,
		effects, exam, findings, p.ReviewComment, nano(p.CreatedAt), nano(p.UpdatedAt),
	)
	if err != nil {
		return wrap("create plan", err)
	}
	return nil
}

func (s *Store) GetPlan(ctx context.Context, id store.PlanID) (store.Plan, error) {
	if err := s.guard(); err != nil {
		return store.Plan{}, err
	}
	if id.IsZero() {
		return store.Plan{}, wrap("get plan", store.ErrInvalid)
	}
	return scanPlan(s.db.QueryRowContext(ctx, `SELECT `+planCols+` FROM plans WHERE id = ?`, id.String()), "get plan")
}

func (s *Store) UpdatePlan(ctx context.Context, p store.Plan) error {
	if err := s.guard(); err != nil {
		return err
	}
	if p.ID.IsZero() || p.SessionID.IsZero() || p.Status == "" || p.Summary == "" {
		return wrap("update plan", store.ErrInvalid)
	}
	effects, exam, findings, err := planJSON(p)
	if err != nil {
		return wrap("update plan", err)
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE plans SET session_id = ?, parent_plan_id = ?, status = ?, summary = ?, effects_json = ?,
			cross_exam_json = ?, secret_scan_findings_json = ?, review_comment = ?, updated_at_unix_nano = ?
		WHERE id = ?`,
		p.SessionID.String(), p.ParentPlanID.String(), p.Status, p.Summary, effects, exam, findings,
		p.ReviewComment, nano(p.UpdatedAt), p.ID.String(),
	)
	if err != nil {
		return wrap("update plan", err)
	}
	return affectedOne(res, "update plan")
}

func (s *Store) ListPlans(ctx context.Context, q store.PlanQuery) (store.Page[store.Plan], error) {
	if err := s.guard(); err != nil {
		return store.Page[store.Plan]{}, err
	}
	limit := store.ClampLimit(q.Limit)
	var conds []string
	var args []any
	if !q.SessionID.IsZero() {
		conds = append(conds, `session_id = ?`)
		args = append(args, q.SessionID.String())
	}
	if q.Status != "" {
		conds = append(conds, `status = ?`)
		args = append(args, q.Status)
	}
	cw, cargs, err := cursorWhere("created_at_unix_nano", listNewestFirst, q.Cursor)
	if err != nil {
		return store.Page[store.Plan]{}, wrap("list plans", err)
	}
	if cw != "" {
		conds = append(conds, cw)
		args = append(args, cargs...)
	}
	query := `SELECT ` + planCols + ` FROM plans`
	if len(conds) > 0 {
		query += ` WHERE ` + strings.Join(conds, " AND ")
	}
	query += ` ORDER BY created_at_unix_nano DESC, id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return store.Page[store.Plan]{}, wrap("list plans", err)
	}
	defer rows.Close()
	items, err := scanPlans(rows)
	if err != nil {
		return store.Page[store.Plan]{}, err
	}
	return pageOf(items, limit, func(p store.Plan) time.Time { return p.CreatedAt }, func(p store.Plan) string { return p.ID.String() }), nil
}

func planJSON(p store.Plan) (effects, exam, findings string, err error) {
	effects, err = marshalEffects(p.Effects)
	if err != nil {
		return "", "", "", err
	}
	exam, err = marshalCrossExam(p.CrossExam)
	if err != nil {
		return "", "", "", err
	}
	findings, err = marshalFindings(p.SecretScanFindings)
	if err != nil {
		return "", "", "", err
	}
	return effects, exam, findings, nil
}

func scanPlan(row *sql.Row, op string) (store.Plan, error) {
	var p store.Plan
	var id, sess, parent, effects, exam, findings string
	var created, updated int64
	err := row.Scan(&id, &sess, &parent, &p.Status, &p.Summary, &effects, &exam, &findings, &p.ReviewComment, &created, &updated)
	if err != nil {
		return store.Plan{}, wrap(op, err)
	}
	return finishPlan(p, id, sess, parent, effects, exam, findings, created, updated, op)
}

func scanPlans(rows *sql.Rows) ([]store.Plan, error) {
	var out []store.Plan
	for rows.Next() {
		var p store.Plan
		var id, sess, parent, effects, exam, findings string
		var created, updated int64
		if err := rows.Scan(&id, &sess, &parent, &p.Status, &p.Summary, &effects, &exam, &findings, &p.ReviewComment, &created, &updated); err != nil {
			return nil, wrap("list plans", err)
		}
		p, err := finishPlan(p, id, sess, parent, effects, exam, findings, created, updated, "list plans")
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap("list plans", err)
	}
	if out == nil {
		out = []store.Plan{}
	}
	return out, nil
}

func finishPlan(p store.Plan, id, sess, parent, effects, exam, findings string, created, updated int64, op string) (store.Plan, error) {
	pid, err := store.ParsePlanID(id)
	if err != nil {
		return store.Plan{}, wrap(op, err)
	}
	sid, err := store.ParseSessionID(sess)
	if err != nil {
		return store.Plan{}, wrap(op, err)
	}
	p.ID = pid
	p.SessionID = sid
	if parent != "" {
		pp, err := store.ParsePlanID(parent)
		if err != nil {
			return store.Plan{}, wrap(op, err)
		}
		p.ParentPlanID = pp
	}
	p.Effects, err = unmarshalEffects(effects)
	if err != nil {
		return store.Plan{}, wrap(op, err)
	}
	p.CrossExam, err = unmarshalCrossExam(exam)
	if err != nil {
		return store.Plan{}, wrap(op, err)
	}
	p.SecretScanFindings, err = unmarshalFindings(findings)
	if err != nil {
		return store.Plan{}, wrap(op, err)
	}
	p.CreatedAt = fromNano(created)
	p.UpdatedAt = fromNano(updated)
	return p, nil
}

func (s *Store) CreateApproval(ctx context.Context, a store.Approval) error {
	if err := s.guard(); err != nil {
		return err
	}
	if a.ID.IsZero() || a.Kind == "" || a.Status == "" {
		return wrap("create approval", store.ErrInvalid)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO approvals (id, kind, status, plan_id, session_id, summary, created_at_unix_nano)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		a.ID.String(), a.Kind, a.Status, a.PlanID.String(), a.SessionID.String(), a.Summary, nano(a.CreatedAt),
	)
	if err != nil {
		return wrap("create approval", err)
	}
	return nil
}

func (s *Store) GetApproval(ctx context.Context, id store.ApprovalID) (store.Approval, error) {
	if err := s.guard(); err != nil {
		return store.Approval{}, err
	}
	if id.IsZero() {
		return store.Approval{}, wrap("get approval", store.ErrInvalid)
	}
	var rawID, plan, sess string
	var created int64
	var a store.Approval
	err := s.db.QueryRowContext(ctx, `
		SELECT id, kind, status, plan_id, session_id, summary, created_at_unix_nano
		FROM approvals WHERE id = ?`, id.String(),
	).Scan(&rawID, &a.Kind, &a.Status, &plan, &sess, &a.Summary, &created)
	if err != nil {
		return store.Approval{}, wrap("get approval", err)
	}
	return finishApproval(a, rawID, plan, sess, created, "get approval")
}

func (s *Store) UpdateApproval(ctx context.Context, a store.Approval) error {
	if err := s.guard(); err != nil {
		return err
	}
	if a.ID.IsZero() || a.Kind == "" || a.Status == "" {
		return wrap("update approval", store.ErrInvalid)
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE approvals SET kind = ?, status = ?, plan_id = ?, session_id = ?, summary = ?
		WHERE id = ?`,
		a.Kind, a.Status, a.PlanID.String(), a.SessionID.String(), a.Summary, a.ID.String(),
	)
	if err != nil {
		return wrap("update approval", err)
	}
	return affectedOne(res, "update approval")
}

func (s *Store) ListApprovals(ctx context.Context, q store.ApprovalQuery) (store.Page[store.Approval], error) {
	if err := s.guard(); err != nil {
		return store.Page[store.Approval]{}, err
	}
	limit := store.ClampLimit(q.Limit)
	kind := listNewestFirst
	order := `ORDER BY created_at_unix_nano DESC, id DESC`
	if q.Status == "pending" {
		kind = listOldestFirst
		order = `ORDER BY created_at_unix_nano ASC, id ASC`
	}
	var conds []string
	var args []any
	if q.Status != "" {
		conds = append(conds, `status = ?`)
		args = append(args, q.Status)
	}
	cw, cargs, err := cursorWhere("created_at_unix_nano", kind, q.Cursor)
	if err != nil {
		return store.Page[store.Approval]{}, wrap("list approvals", err)
	}
	if cw != "" {
		conds = append(conds, cw)
		args = append(args, cargs...)
	}
	query := `SELECT id, kind, status, plan_id, session_id, summary, created_at_unix_nano FROM approvals`
	if len(conds) > 0 {
		query += ` WHERE ` + strings.Join(conds, " AND ")
	}
	query += ` ` + order + ` LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return store.Page[store.Approval]{}, wrap("list approvals", err)
	}
	defer rows.Close()
	var items []store.Approval
	for rows.Next() {
		var a store.Approval
		var rawID, plan, sess string
		var created int64
		if err := rows.Scan(&rawID, &a.Kind, &a.Status, &plan, &sess, &a.Summary, &created); err != nil {
			return store.Page[store.Approval]{}, wrap("list approvals", err)
		}
		a, err := finishApproval(a, rawID, plan, sess, created, "list approvals")
		if err != nil {
			return store.Page[store.Approval]{}, err
		}
		items = append(items, a)
	}
	if err := rows.Err(); err != nil {
		return store.Page[store.Approval]{}, wrap("list approvals", err)
	}
	return pageOf(items, limit, func(a store.Approval) time.Time { return a.CreatedAt }, func(a store.Approval) string { return a.ID.String() }), nil
}

func finishApproval(a store.Approval, rawID, plan, sess string, created int64, op string) (store.Approval, error) {
	id, err := store.ParseApprovalID(rawID)
	if err != nil {
		return store.Approval{}, wrap(op, err)
	}
	a.ID = id
	if plan != "" {
		pid, err := store.ParsePlanID(plan)
		if err != nil {
			return store.Approval{}, wrap(op, err)
		}
		a.PlanID = pid
	}
	if sess != "" {
		sid, err := store.ParseSessionID(sess)
		if err != nil {
			return store.Approval{}, wrap(op, err)
		}
		a.SessionID = sid
	}
	a.CreatedAt = fromNano(created)
	return a, nil
}
