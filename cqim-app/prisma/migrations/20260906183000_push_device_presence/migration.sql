CREATE TABLE "PushDeviceToken" (
    "id" TEXT NOT NULL,
    "userId" TEXT NOT NULL,
    "token" TEXT NOT NULL,
    "platform" TEXT NOT NULL,
    "environment" TEXT NOT NULL,
    "kind" TEXT NOT NULL DEFAULT 'alert',
    "appVersion" TEXT,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,

    CONSTRAINT "PushDeviceToken_pkey" PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX "PushDeviceToken_token_kind_key" ON "PushDeviceToken"("token", "kind");
CREATE INDEX "PushDeviceToken_userId_platform_environment_kind_idx" ON "PushDeviceToken"("userId", "platform", "environment", "kind");
ALTER TABLE "PushDeviceToken" ADD CONSTRAINT "PushDeviceToken_userId_fkey"
  FOREIGN KEY ("userId") REFERENCES "User"("id") ON DELETE CASCADE ON UPDATE CASCADE;
