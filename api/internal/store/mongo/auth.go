package mongostore

import (
	"context"
	"errors"
	"time"

	"github.com/aribpos/license-api/internal/model"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// --- OTP ---

// UpsertOTP replaces any pending OTP for the email with a fresh one.
func (s *Store) UpsertOTP(ctx context.Context, o *model.OTP) error {
	o.Email = normEmail(o.Email)
	_, err := s.OTPs.ReplaceOne(ctx,
		bson.D{{Key: "email", Value: o.Email}}, o,
		options.Replace().SetUpsert(true))
	return err
}

// LatestOTP returns the current OTP for an email.
func (s *Store) LatestOTP(ctx context.Context, email string) (*model.OTP, error) {
	var o model.OTP
	err := s.OTPs.FindOne(ctx, bson.D{{Key: "email", Value: normEmail(email)}}).Decode(&o)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	return &o, err
}

// IncOTPAttempts atomically increments the failed-attempt counter.
func (s *Store) IncOTPAttempts(ctx context.Context, id string) error {
	_, err := s.OTPs.UpdateByID(ctx, id, bson.D{{Key: "$inc", Value: bson.D{{Key: "attempts", Value: 1}}}})
	return err
}

// DeleteOTP removes a consumed OTP.
func (s *Store) DeleteOTP(ctx context.Context, id string) error {
	_, err := s.OTPs.DeleteOne(ctx, bson.D{{Key: "_id", Value: id}})
	return err
}

// --- Sessions (refresh tokens) ---

// InsertSession stores a refresh-token record.
func (s *Store) InsertSession(ctx context.Context, ses *model.Session) error {
	_, err := s.Sessions.InsertOne(ctx, ses)
	return err
}

// SessionByTokenHash looks up a session by its hashed refresh token.
func (s *Store) SessionByTokenHash(ctx context.Context, hash string) (*model.Session, error) {
	var ses model.Session
	err := s.Sessions.FindOne(ctx, bson.D{{Key: "token_hash", Value: hash}}).Decode(&ses)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	return &ses, err
}

// RotateSession swaps an old refresh-token hash for a new one (refresh flow).
func (s *Store) RotateSession(ctx context.Context, id, newHash string, expiresAt time.Time) error {
	_, err := s.Sessions.UpdateByID(ctx, id, bson.D{{Key: "$set", Value: bson.D{
		{Key: "token_hash", Value: newHash},
		{Key: "expires_at", Value: expiresAt},
		{Key: "last_used_at", Value: time.Now().UTC()},
	}}})
	return err
}

// DeleteSession revokes a session (logout).
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	_, err := s.Sessions.DeleteOne(ctx, bson.D{{Key: "_id", Value: id}})
	return err
}

// --- OAuth one-time exchange codes ---

// InsertExchange stores a one-time OAuth exchange code.
func (s *Store) InsertExchange(ctx context.Context, e *model.OAuthExchange) error {
	_, err := s.Exchanges.InsertOne(ctx, e)
	return err
}

// ConsumeExchange atomically fetches and deletes an exchange code.
func (s *Store) ConsumeExchange(ctx context.Context, code string) (*model.OAuthExchange, error) {
	var e model.OAuthExchange
	err := s.Exchanges.FindOneAndDelete(ctx, bson.D{{Key: "_id", Value: code}}).Decode(&e)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	return &e, err
}

// --- Trial ledger ---

// TrialUsed reports whether a machine has already consumed a trial.
func (s *Store) TrialUsed(ctx context.Context, machineID string) (bool, error) {
	err := s.Trials.FindOne(ctx, bson.D{{Key: "_id", Value: machineID}}).Err()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// RecordTrial marks a machine as having used its trial (idempotent).
func (s *Store) RecordTrial(ctx context.Context, t *model.TrialLedger) error {
	_, err := s.Trials.InsertOne(ctx, t)
	if IsDuplicateKey(err) {
		return nil
	}
	return err
}

// --- Audit ---

// InsertAudit appends an audit record.
func (s *Store) InsertAudit(ctx context.Context, a *model.AuditLog) error {
	_, err := s.Audit.InsertOne(ctx, a)
	return err
}

func findLimitSorted(limit int64) *options.FindOptionsBuilder {
	return options.Find().SetLimit(limit).SetSort(bson.D{{Key: "created_at", Value: -1}})
}
