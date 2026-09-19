-- Add scalar User fields present in the PostgreSQL Prisma schema.
ALTER TABLE "User" ADD COLUMN IF NOT EXISTS "gender" TEXT;
ALTER TABLE "User" ADD COLUMN IF NOT EXISTS "region" TEXT;
ALTER TABLE "User" ADD COLUMN IF NOT EXISTS "birthday" TEXT;
