ALTER TABLE "Chat"
  ADD COLUMN "vanishMode" BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN "vanishSeconds" INTEGER,
  ADD COLUMN "restrictForwarding" BOOLEAN NOT NULL DEFAULT false;
