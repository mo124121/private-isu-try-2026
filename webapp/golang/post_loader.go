package main

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
)

type commentCountRow struct {
	PostID int `db:"post_id"`
	Count  int `db:"count"`
}

// loadPosts は一覧・詳細画面で必要な関連データをまとめて取得して Post を組み立てます。
// conn はテスト時にも同じ DB アクセス経路を使えるように明示的に受け取ります。
func loadPosts(ctx context.Context, conn *sqlx.DB, results []Post, csrfToken string, allComments bool) ([]Post, error) {
	ownerIDs := make([]int, 0, len(results))
	for _, post := range results {
		ownerIDs = append(ownerIDs, post.UserID)
	}

	ownerUsers, err := loadUsers(ctx, conn, ownerIDs)
	if err != nil {
		return nil, err
	}

	visibleResults := make([]Post, 0, postsPerPage)
	for _, post := range results {
		owner, ok := ownerUsers[post.UserID]
		if !ok {
			return nil, fmt.Errorf("user %d for post %d was not found", post.UserID, post.ID)
		}
		if owner.DelFlg != 0 {
			continue
		}

		post.User = owner
		visibleResults = append(visibleResults, post)
		if len(visibleResults) >= postsPerPage {
			break
		}
	}

	postIDs := make([]int, 0, len(visibleResults))
	for _, post := range visibleResults {
		postIDs = append(postIDs, post.ID)
	}

	commentCounts, err := loadCommentCounts(ctx, conn, postIDs)
	if err != nil {
		return nil, err
	}
	commentsByPostID, err := loadComments(ctx, conn, postIDs, allComments)
	if err != nil {
		return nil, err
	}

	commentUserIDs := make([]int, 0)
	for _, comments := range commentsByPostID {
		for _, comment := range comments {
			commentUserIDs = append(commentUserIDs, comment.UserID)
		}
	}
	commentUsers, err := loadUsers(ctx, conn, commentUserIDs)
	if err != nil {
		return nil, err
	}

	for _, comments := range commentsByPostID {
		for _, comment := range comments {
			if _, ok := commentUsers[comment.UserID]; !ok {
				return nil, fmt.Errorf("user %d for comment %d was not found", comment.UserID, comment.ID)
			}
		}
	}

	return assemblePosts(visibleResults, commentCounts, commentsByPostID, commentUsers, csrfToken), nil
}

func loadCommentCounts(ctx context.Context, conn *sqlx.DB, postIDs []int) (map[int]int, error) {
	counts := make(map[int]int)
	postIDs = uniqueIDs(postIDs)
	if len(postIDs) == 0 {
		return counts, nil
	}

	query, args, err := sqlx.In(`
		SELECT post_id, COUNT(*) AS count
		FROM comments
		WHERE post_id IN (?)
		GROUP BY post_id`, postIDs)
	if err != nil {
		return nil, fmt.Errorf("build comment count query: %w", err)
	}

	var rows []commentCountRow
	if err := conn.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, fmt.Errorf("load comment counts: %w", err)
	}
	for _, row := range rows {
		counts[row.PostID] = row.Count
	}
	return counts, nil
}

func loadComments(ctx context.Context, conn *sqlx.DB, postIDs []int, allComments bool) (map[int][]Comment, error) {
	commentsByPostID := make(map[int][]Comment)
	postIDs = uniqueIDs(postIDs)
	if len(postIDs) == 0 {
		return commentsByPostID, nil
	}

	query := `
		SELECT id, post_id, user_id, comment, created_at
		FROM comments
		WHERE post_id IN (?)
		ORDER BY post_id ASC, created_at ASC, id ASC`
	if !allComments {
		query = `
			SELECT id, post_id, user_id, comment, created_at
			FROM (
				SELECT id, post_id, user_id, comment, created_at,
					ROW_NUMBER() OVER (
						PARTITION BY post_id
						ORDER BY created_at DESC, id DESC
					) AS row_num
				FROM comments
				WHERE post_id IN (?)
			) AS ranked_comments
			WHERE row_num <= 3
			ORDER BY post_id ASC, created_at ASC, id ASC`
	}

	query, args, err := sqlx.In(query, postIDs)
	if err != nil {
		return nil, fmt.Errorf("build comments query: %w", err)
	}

	var rows []Comment
	if err := conn.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, fmt.Errorf("load comments: %w", err)
	}
	for _, comment := range rows {
		commentsByPostID[comment.PostID] = append(commentsByPostID[comment.PostID], comment)
	}
	return commentsByPostID, nil
}

func loadUsers(ctx context.Context, conn *sqlx.DB, userIDs []int) (map[int]User, error) {
	usersByID := make(map[int]User)
	userIDs = uniqueIDs(userIDs)
	if len(userIDs) == 0 {
		return usersByID, nil
	}

	query, args, err := sqlx.In("SELECT * FROM users WHERE id IN (?)", userIDs)
	if err != nil {
		return nil, fmt.Errorf("build users query: %w", err)
	}

	var rows []User
	if err := conn.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, fmt.Errorf("load users: %w", err)
	}
	for _, user := range rows {
		usersByID[user.ID] = user
	}
	return usersByID, nil
}

func assemblePosts(results []Post, commentCounts map[int]int, commentsByPostID map[int][]Comment, commentUsers map[int]User, csrfToken string) []Post {
	posts := make([]Post, 0, len(results))
	for _, post := range results {
		post.CommentCount = commentCounts[post.ID]
		post.Comments = commentsByPostID[post.ID]
		for i := range post.Comments {
			post.Comments[i].User = commentUsers[post.Comments[i].UserID]
		}
		post.CSRFToken = csrfToken
		posts = append(posts, post)
	}
	return posts
}

func uniqueIDs(ids []int) []int {
	seen := make(map[int]struct{}, len(ids))
	result := make([]int, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}
