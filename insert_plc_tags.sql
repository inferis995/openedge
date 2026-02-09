-- Delete existing tags for gateway 1 to start fresh
DELETE FROM tags WHERE gateway_id = 1;

-- 1. DIGITALI: Uscite (Coils)
INSERT INTO tags (gateway_id, code, alias, data_type, historize, historize_deadband) VALUES
(1, '00001', 'DO_Valvola_1', 'BOOL', false, 0),
(1, '00002', 'DO_Valvola_2', 'BOOL', false, 0),
(1, '00003', 'DO_Valvola_3', 'BOOL', false, 0),
(1, '00004', 'DO_Valvola_4', 'BOOL', false, 0),
(1, '00005', 'HMI_CMD_Valvola_1', 'BOOL', false, 0),
(1, '00006', 'HMI_CMD_Valvola_2', 'BOOL', false, 0),
(1, '00007', 'HMI_CMD_Valvola_3', 'BOOL', false, 0),
(1, '00008', 'HMI_CMD_Valvola_4', 'BOOL', false, 0),
(1, '00009', 'HMI_CFG_EnableValvola_1', 'BOOL', false, 0),
(1, '00010', 'HMI_CFG_EnableValvola_2', 'BOOL', false, 0),
(1, '00011', 'HMI_CFG_EnableValvola_3', 'BOOL', false, 0),
(1, '00012', 'HMI_CFG_EnableValvola_4', 'BOOL', false, 0),
(1, '00013', 'HMI_ResetTotClampOn1', 'BOOL', false, 0),
(1, '00014', 'HMI_ResetTotClampOn2', 'BOOL', false, 0),
(1, '00015', 'HMI_ResetTotClampOn3', 'BOOL', false, 0),
(1, '00016', 'HMI_ResetTotClampOn4', 'BOOL', false, 0),
(1, '00017', 'HMI_ResetTotClampOn5', 'BOOL', false, 0),
(1, '00018', 'HMI_ResetTotEM1', 'BOOL', false, 0),
(1, '00019', 'HMI_ResetTotEM2', 'BOOL', false, 0),
(1, '00020', 'HMI_ResetTotEM3', 'BOOL', false, 0),
(1, '00021', 'HMI_ResetTotEM4', 'BOOL', false, 0),
(1, '00022', 'HMI_ResetTotEM5', 'BOOL', false, 0),
(1, '00023', 'HMI_Set_DateAndTime', 'BOOL', false, 0);

-- 2. DIGITALI: Ingressi (Discrete Inputs)
INSERT INTO tags (gateway_id, code, alias, data_type, historize, historize_deadband) VALUES
(1, '10001', 'Totalizzatore_CLAMPON_1', 'BOOL', false, 0),
(1, '10002', 'Totalizzatore_CLAMPON_2', 'BOOL', false, 0),
(1, '10003', 'Totalizzatore_CLAMPON_3', 'BOOL', false, 0),
(1, '10004', 'Totalizzatore_CLAMPON_4', 'BOOL', false, 0),
(1, '10005', 'Totalizzatore_CLAMPON_5', 'BOOL', false, 0),
(1, '10006', 'HMI_FC_Aperta_Valvola_1', 'BOOL', false, 0),
(1, '10007', 'HMI_FC_Chiusa_Valvola_1', 'BOOL', false, 0),
(1, '10008', 'HMI_FC_Aperta_Valvola_2', 'BOOL', false, 0),
(1, '10009', 'HMI_FC_Chiusa_Valvola_2', 'BOOL', false, 0),
(1, '10010', 'HMI_FC_Aperta_Valvola_3', 'BOOL', false, 0),
(1, '10011', 'HMI_FC_Chiusa_Valvola_3', 'BOOL', false, 0),
(1, '10012', 'HMI_FC_Aperta_Valvola_4', 'BOOL', false, 0),
(1, '10013', 'HMI_FC_Chiusa_Valvola_4', 'BOOL', false, 0),
(1, '10014', 'Totalizzatore_EM_1', 'BOOL', false, 0),
(1, '10015', 'Totalizzatore_EM_2', 'BOOL', false, 0),
(1, '10016', 'Totalizzatore_EM_3', 'BOOL', false, 0),
(1, '10017', 'Totalizzatore_EM_4', 'BOOL', false, 0),
(1, '10018', 'Totalizzatore_EM_5', 'BOOL', false, 0);

-- 3. ANALOGICI: Letture Istantanee (Input Registers)
INSERT INTO tags (gateway_id, code, alias, data_type, historize, historize_deadband) VALUES
(1, '30001', 'Portata_Misuratore_CLAMP_ON_1', 'INT', true, 1),
(1, '30002', 'Portata_Misuratore_CLAMP_ON_2', 'INT', true, 1),
(1, '30003', 'Portata_Misuratore_CLAMP_ON_3', 'INT', true, 1),
(1, '30004', 'Portata_Misuratore_CLAMP_ON_4', 'INT', true, 1),
(1, '30005', 'Portata_Misuratore_CLAMP_ON_5', 'INT', true, 1),
(1, '30006', 'Portata_Misuratore_EM_1', 'INT', true, 1),
(1, '30007', 'Portata_Misuratore_EM_2', 'INT', true, 1),
(1, '30008', 'Portata_Misuratore_EM_3', 'INT', true, 1),
(1, '30009', 'Portata_Misuratore_EM_4', 'INT', true, 1),
(1, '30010', 'Portata_Misuratore_EM_5', 'INT', true, 1),
(1, '30011', 'LivelloRadar', 'INT', true, 1);

-- 4. PARAMETRI E CALCOLI (Holding Registers - INT)
INSERT INTO tags (gateway_id, code, alias, data_type, historize, historize_deadband) VALUES
(1, '40001', 'HMI_ModSel', 'INT', false, 0),
(1, '40002', 'HMI_CFG_Seconds', 'INT', false, 0),
(1, '40003', 'HMI_CFG_Hour_Minute', 'INT', false, 0),
(1, '40004', 'HMI_CFG_Month_Day', 'INT', false, 0),
(1, '40005', 'HMI_CFG_Year', 'INT', false, 0),
(1, '40006', 'HMI_CFG_PVTotClampOn1', 'INT', false, 0),
(1, '40007', 'HMI_CFG_PVTotClampOn2', 'INT', false, 0),
(1, '40008', 'HMI_CFG_PVTotClampOn3', 'INT', false, 0),
(1, '40009', 'HMI_CFG_PVTotClampOn4', 'INT', false, 0),
(1, '40010', 'HMI_CFG_PVTotClampOn5', 'INT', false, 0),
(1, '40011', 'HMI_CFG_PVTotEM1', 'INT', false, 0),
(1, '40012', 'HMI_CFG_PVTotEM2', 'INT', false, 0),
(1, '40013', 'HMI_CFG_PVTotEM3', 'INT', false, 0),
(1, '40014', 'HMI_CFG_PVTotEM4', 'INT', false, 0),
(1, '40015', 'HMI_CFG_PVTotEM5', 'INT', false, 0),
(1, '40016', 'HMI_CFG_DBeg_1', 'INT', false, 0),
(1, '40017', 'HMI_CFG_DEnd_1', 'INT', false, 0),
(1, '40018', 'HMI_CFG_Week_1', 'INT', false, 0),
(1, '40019', 'HMI_CFG_DBeg_2', 'INT', false, 0),
(1, '40020', 'HMI_CFG_DEnd_2', 'INT', false, 0),
(1, '40021', 'HMI_CFG_Week_2', 'INT', false, 0),
(1, '40022', 'HMI_CFG_DBeg_3', 'INT', false, 0),
(1, '40023', 'HMI_CFG_DEnd_3', 'INT', false, 0),
(1, '40024', 'HMI_CFG_Week_3', 'INT', false, 0),
(1, '40025', 'HMI_CFG_DBeg_4', 'INT', false, 0),
(1, '40026', 'HMI_CFG_DEnd_4', 'INT', false, 0),
(1, '40027', 'HMI_CFG_Week_4', 'INT', false, 0);

-- 5. LETTURE DATE/ORA (Memory - INT)
INSERT INTO tags (gateway_id, code, alias, data_type, historize, historize_deadband) VALUES
(1, '40101', 'HMI_Read_Seconds', 'INT', false, 0),
(1, '40102', 'HMI_Read_Hour_Minute', 'INT', false, 0),
(1, '40103', 'HMI_Read_Month_Day', 'INT', false, 0),
(1, '40104', 'HMI_Read_Year', 'INT', false, 0);

-- 6. SCALATURE (Holding Registers - REAL - 2 registri)
INSERT INTO tags (gateway_id, code, alias, data_type, historize, historize_deadband) VALUES
(1, '40201', 'HMI_CFG_ScalaturaPortClampOn1', 'REAL', false, 0),
(1, '40203', 'HMI_CFG_ScalaturaPortClampOn2', 'REAL', false, 0),
(1, '40205', 'HMI_CFG_ScalaturaPortClampOn3', 'REAL', false, 0),
(1, '40207', 'HMI_CFG_ScalaturaPortClampOn4', 'REAL', false, 0),
(1, '40209', 'HMI_CFG_ScalaturaPortClampOn5', 'REAL', false, 0),
(1, '40211', 'HMI_CFG_ScalaturaTotClampOn1', 'REAL', false, 0),
(1, '40213', 'HMI_CFG_ScalaturaTotClampOn2', 'REAL', false, 0),
(1, '40215', 'HMI_CFG_ScalaturaTotClampOn3', 'REAL', false, 0),
(1, '40217', 'HMI_CFG_ScalaturaTotClampOn4', 'REAL', false, 0),
(1, '40219', 'HMI_CFG_ScalaturaTotClampOn5', 'REAL', false, 0),
(1, '40221', 'HMI_CFG_ScalaturaPortEM1', 'REAL', false, 0),
(1, '40223', 'HMI_CFG_ScalaturaPortEM2', 'REAL', false, 0),
(1, '40225', 'HMI_CFG_ScalaturaPortEM3', 'REAL', false, 0),
(1, '40227', 'HMI_CFG_ScalaturaPortEM4', 'REAL', false, 0),
(1, '40229', 'HMI_CFG_ScalaturaPortEM5', 'REAL', false, 0),
(1, '40231', 'HMI_CFG_ScalaturaTotEM1', 'REAL', false, 0),
(1, '40233', 'HMI_CFG_ScalaturaTotEM2', 'REAL', false, 0),
(1, '40235', 'HMI_CFG_ScalaturaTotEM3', 'REAL', false, 0),
(1, '40237', 'HMI_CFG_ScalaturaTotEM4', 'REAL', false, 0),
(1, '40239', 'HMI_CFG_ScalaturaTotEM5', 'REAL', false, 0),
(1, '40241', 'HMI_CFG_ScalaturaLivRadar', 'REAL', false, 0),
(1, '40243', 'HMI_CFG_SogliaAltaLvlRadar', 'REAL', false, 0),
(1, '40245', 'HMI_CFG_SogliaBassaLvlRadar', 'REAL', false, 0);

-- DINT values (occupy 2 registers like REAL)
INSERT INTO tags (gateway_id, code, alias, data_type, historize, historize_deadband) VALUES
(1, '40247', 'HMI_CFG_HBeg_1', 'DINT', false, 0),
(1, '40249', 'HMI_CFG_HEnd_1', 'DINT', false, 0),
(1, '40251', 'HMI_CFG_HBeg_2', 'DINT', false, 0),
(1, '40253', 'HMI_CFG_HEnd_2', 'DINT', false, 0),
(1, '40255', 'HMI_CFG_HBeg_3', 'DINT', false, 0),
(1, '40257', 'HMI_CFG_HEnd_3', 'DINT', false, 0),
(1, '40259', 'HMI_CFG_HBeg_4', 'DINT', false, 0),
(1, '40261', 'HMI_CFG_HEnd_4', 'DINT', false, 0);

-- 7. DATI ISTANTANEI HMI (Holding Registers - REAL - Read Only - 2 registri)
INSERT INTO tags (gateway_id, code, alias, data_type, historize, historize_deadband) VALUES
(1, '40263', 'HMI_PortataTotClampOn1', 'REAL', true, 0.1),
(1, '40265', 'HMI_PortataTotClampOn2', 'REAL', true, 0.1),
(1, '40267', 'HMI_PortataTotClampOn3', 'REAL', true, 0.1),
(1, '40269', 'HMI_PortataTotClampOn4', 'REAL', true, 0.1),
(1, '40271', 'HMI_PortataTotClampOn5', 'REAL', true, 0.1),
(1, '40273', 'HMI_PortInstClampOn1', 'REAL', true, 0.1),
(1, '40275', 'HMI_PortInstClampOn2', 'REAL', true, 0.1),
(1, '40277', 'HMI_PortInstClampOn3', 'REAL', true, 0.1),
(1, '40279', 'HMI_PortInstClampOn4', 'REAL', true, 0.1),
(1, '40281', 'HMI_PortInstClampOn5', 'REAL', true, 0.1),
(1, '40283', 'HMI_PortataTotEM1', 'REAL', true, 0.1),
(1, '40285', 'HMI_PortataTotEM2', 'REAL', true, 0.1),
(1, '40287', 'HMI_PortataTotEM3', 'REAL', true, 0.1),
(1, '40289', 'HMI_PortataTotEM4', 'REAL', true, 0.1),
(1, '40291', 'HMI_PortataTotEM5', 'REAL', true, 0.1),
(1, '40293', 'HMI_PortInstEM1', 'REAL', true, 0.1),
(1, '40295', 'HMI_PortInstEM2', 'REAL', true, 0.1),
(1, '40297', 'HMI_PortInstEM3', 'REAL', true, 0.1),
(1, '40299', 'HMI_PortInstEM4', 'REAL', true, 0.1),
(1, '40301', 'HMI_PortInstEM5', 'REAL', true, 0.1),
(1, '40303', 'HMI_LivelloRadarScalato', 'REAL', true, 0.1);
