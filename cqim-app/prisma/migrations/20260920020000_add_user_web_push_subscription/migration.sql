-- Add User.webPushSubscription present in the Prisma schema (nullable Web Push JSON).
-- IF NOT EXISTS keeps this safe on production, which already received the same hotfix.
ALTER TABLE "User" ADD COLUMN IF NOT EXISTS "webPushSubscription" TEXT;
