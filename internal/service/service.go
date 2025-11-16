package service

import (
	"context"
	"errors"
	"math/rand"
	"review-service/internal/database"
	"review-service/internal/models"
	"time"
)

type Service struct {
	db *database.DB
}

func NewService(db *database.DB) *Service {
	return &Service{db: db}
}

// Team methods
func (s *Service) CreateTeam(ctx context.Context, req models.CreateTeamRequest) (*models.Team, error) {
	// Check if team already exists
	existingTeam, _ := s.db.GetTeamByName(ctx, req.TeamName)
	if existingTeam != nil {
		return nil, ErrTeamExists
	}

	// Create team
	team := &models.Team{
		TeamName: req.TeamName,
		Members:  req.Members,
	}

	if err := s.db.CreateTeam(ctx, team); err != nil {
		return nil, err
	}

	// Create/update users
	for _, member := range req.Members {
		user := &models.User{
			UserID:   member.UserID,
			Username: member.Username,
			TeamName: req.TeamName,
			IsActive: member.IsActive,
		}
		if err := s.db.CreateOrUpdateUser(ctx, user); err != nil {
			return nil, err
		}
	}

	return team, nil
}

func (s *Service) GetTeam(ctx context.Context, teamName string) (*models.Team, error) {
	team, err := s.db.GetTeamByName(ctx, teamName)
	if err != nil {
		return nil, ErrTeamNotFound
	}
	return team, nil
}

// User methods
func (s *Service) SetUserActive(ctx context.Context, req models.SetUserActiveRequest) (*models.User, error) {
	user, err := s.db.GetUserByID(ctx, req.UserID)
	if err != nil {
		return nil, ErrUserNotFound
	}

	user.IsActive = req.IsActive
	if err := s.db.UpdateUser(ctx, user); err != nil {
		return nil, err
	}

	return user, nil
}

// PR methods
func (s *Service) CreatePR(ctx context.Context, req models.CreatePRRequest) (*models.PullRequest, error) {
	// Check if PR already exists
	existingPR, _ := s.db.GetPRByID(ctx, req.PullRequestID)
	if existingPR != nil {
		return nil, ErrPRExists
	}

	// Get author
	author, err := s.db.GetUserByID(ctx, req.AuthorID)
	if err != nil {
		return nil, ErrUserNotFound
	}

	// Get team members for reviewers
	teamMembers, err := s.db.GetActiveUsersByTeam(ctx, author.TeamName, author.UserID)
	if err != nil {
		return nil, err
	}

	// Select up to 2 random reviewers
	var reviewers []string
	if len(teamMembers) > 0 {
		rand.Shuffle(len(teamMembers), func(i, j int) {
			teamMembers[i], teamMembers[j] = teamMembers[j], teamMembers[i]
		})

		count := min(2, len(teamMembers))
		for i := 0; i < count; i++ {
			reviewers = append(reviewers, teamMembers[i].UserID)
		}
	}

	now := time.Now()
	pr := &models.PullRequest{
		PullRequestID:     req.PullRequestID,
		PullRequestName:   req.PullRequestName,
		AuthorID:          req.AuthorID,
		Status:            models.PRStatusOpen,
		AssignedReviewers: reviewers,
		CreatedAt:         &now,
	}

	if err := s.db.CreatePR(ctx, pr); err != nil {
		return nil, err
	}

	return pr, nil
}

func (s *Service) MergePR(ctx context.Context, prID string) (*models.PullRequest, error) {
	pr, err := s.db.GetPRByID(ctx, prID)
	if err != nil {
		return nil, ErrPRNotFound
	}

	// Idempotent - if already merged, return current state
	if pr.Status == models.PRStatusMerged {
		return pr, nil
	}

	now := time.Now()
	pr.Status = models.PRStatusMerged
	pr.MergedAt = &now

	if err := s.db.UpdatePR(ctx, pr); err != nil {
		return nil, err
	}

	return pr, nil
}

func (s *Service) ReassignReviewer(ctx context.Context, req models.ReassignReviewerRequest) (*models.PullRequest, string, error) {
	pr, err := s.db.GetPRByID(ctx, req.PullRequestID)
	if err != nil {
		return nil, "", ErrPRNotFound
	}

	if pr.Status == models.PRStatusMerged {
		return nil, "", ErrPRMerged
	}

	// Check if old reviewer is assigned
	found := false
	for _, reviewer := range pr.AssignedReviewers {
		if reviewer == req.OldUserID {
			found = true
			break
		}
	}
	if !found {
		return nil, "", ErrReviewerNotAssigned
	}

	// Get old reviewer's team
	oldReviewer, err := s.db.GetUserByID(ctx, req.OldUserID)
	if err != nil {
		return nil, "", ErrUserNotFound
	}

	// Get available replacement candidates
	candidates, err := s.db.GetActiveUsersByTeam(ctx, oldReviewer.TeamName, pr.AuthorID)
	if err != nil {
		return nil, "", err
	}

	// Filter out current reviewers and old reviewer
	var available []models.User
	for _, candidate := range candidates {
		isCurrent := false
		for _, reviewer := range pr.AssignedReviewers {
			if candidate.UserID == reviewer {
				isCurrent = true
				break
			}
		}
		if !isCurrent && candidate.UserID != req.OldUserID {
			available = append(available, candidate)
		}
	}

	if len(available) == 0 {
		return nil, "", ErrNoCandidate
	}

	// Select random replacement
	newReviewer := available[rand.Intn(len(available))]

	// Replace reviewer
	newReviewers := make([]string, len(pr.AssignedReviewers))
	for i, reviewer := range pr.AssignedReviewers {
		if reviewer == req.OldUserID {
			newReviewers[i] = newReviewer.UserID
		} else {
			newReviewers[i] = reviewer
		}
	}
	pr.AssignedReviewers = newReviewers

	if err := s.db.UpdatePRReviewers(ctx, pr.PullRequestID, newReviewers); err != nil {
		return nil, "", err
	}

	return pr, newReviewer.UserID, nil
}

func (s *Service) GetUserPRs(ctx context.Context, userID string) (*models.UserPRsResponse, error) {
	prs, err := s.db.GetPRsByReviewer(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Convert to short format
	var shortPRs []models.PullRequestShort
	for _, pr := range prs {
		shortPRs = append(shortPRs, models.PullRequestShort{
			PullRequestID:   pr.PullRequestID,
			PullRequestName: pr.PullRequestName,
			AuthorID:        pr.AuthorID,
			Status:          pr.Status,
		})
	}

	return &models.UserPRsResponse{
		UserID:       userID,
		PullRequests: shortPRs,
	}, nil
}

func (s *Service) CheckHealth(ctx context.Context) error {
	return s.db.HealthCheck(ctx)
}

// Errors matching OpenAPI spec
var (
	ErrTeamExists          = errors.New("TEAM_EXISTS")
	ErrTeamNotFound        = errors.New("NOT_FOUND")
	ErrUserNotFound        = errors.New("NOT_FOUND")
	ErrPRExists            = errors.New("PR_EXISTS")
	ErrPRNotFound          = errors.New("NOT_FOUND")
	ErrPRMerged            = errors.New("PR_MERGED")
	ErrReviewerNotAssigned = errors.New("NOT_ASSIGNED")
	ErrNoCandidate         = errors.New("NO_CANDIDATE")
)
