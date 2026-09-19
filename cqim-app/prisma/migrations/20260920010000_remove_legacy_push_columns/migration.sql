-- APNs alert/VoIP tokens live exclusively in PushDeviceToken.
ALTER TABLE "User" DROP COLUMN IF EXISTS "fcmToken";
ALTER TABLE "User" DROP COLUMN IF EXISTS "voipToken";
