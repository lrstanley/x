// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package httpcretry

import "context"

type contextKey string

const retryKey contextKey = "retry"

// WithRetry returns a copy of ctx that controls whether retries are performed for
// the associated request. When retry is true, retries are allowed; when retry is
// false, retries are disabled. Requests without an explicit value default to
// allowing retries. Child contexts override parent values.
func WithRetry(ctx context.Context, retry bool) context.Context {
	return context.WithValue(ctx, retryKey, retry)
}

func retryFromContext(ctx context.Context) bool {
	retry, ok := ctx.Value(retryKey).(bool)
	if !ok {
		return true
	}
	return retry
}
