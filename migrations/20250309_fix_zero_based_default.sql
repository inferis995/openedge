-- Fix zero_based default to FALSE (Standard Modbus is 1-based addressing)
-- Previous default of TRUE was incorrect for standard Modbus addressing

-- First, update existing gateways that might have incorrectly inherited TRUE default
-- Only gateways that were created without explicit zero_based value would be affected
-- Since standard Modbus is 1-based, we set zero_based to FALSE for all MODBUS_TCP gateways
UPDATE gateways
SET zero_based = FALSE
WHERE driver_type = 'MODBUS_TCP' AND zero_based IS TRUE;

-- Now alter the column to have the correct default
ALTER TABLE gateways
ALTER COLUMN zero_based SET DEFAULT FALSE;
