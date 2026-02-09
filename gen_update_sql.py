
# Script to generate SQL update for tags based on DEFINITIVE MAP
filename = "update_tags_definitive.sql"

def get_alias_qw(offset):
    # 40001-40027 (Offset 0-26)
    mapping = {
        0: "HMI_ModSel", 1: "HMI_CFG_Seconds", 2: "HMI_CFG_Hour_Minute", 3: "HMI_CFG_Month_Day", 4: "HMI_CFG_Year",
        5: "HMI_CFG_PVTotClampOn1", 6: "HMI_CFG_PVTotClampOn2", 7: "HMI_CFG_PVTotClampOn3", 8: "HMI_CFG_PVTotClampOn4", 9: "HMI_CFG_PVTotClampOn5",
        10: "HMI_CFG_PVTotEM1", 11: "HMI_CFG_PVTotEM2", 12: "HMI_CFG_PVTotEM3", 13: "HMI_CFG_PVTotEM4", 14: "HMI_CFG_PVTotEM5",
        15: "HMI_CFG_DBeg_1", 16: "HMI_CFG_DEnd_1", 17: "HMI_CFG_Week_1",
        18: "HMI_CFG_DBeg_2", 19: "HMI_CFG_DEnd_2", 20: "HMI_CFG_Week_2",
        21: "HMI_CFG_DBeg_3", 22: "HMI_CFG_DEnd_3", 23: "HMI_CFG_Week_3",
        24: "HMI_CFG_DBeg_4", 25: "HMI_CFG_DEnd_4", 26: "HMI_CFG_Week_4"
    }
    return mapping.get(offset, f"HMI_QW_{offset}")

def get_alias_md(code_int):
    # REAL: 42049-42094
    # DINT: 42095-42110
    # REAL: 42111-42152
    
    # 2048-2089 (REAL)
    if code_int == 42049: return "HMI_CFG_ScalaturaPortClampOn1"
    if code_int == 42051: return "HMI_CFG_ScalaturaPortClampOn2"
    if code_int == 42053: return "HMI_CFG_ScalaturaPortClampOn3"
    if code_int == 42055: return "HMI_CFG_ScalaturaPortClampOn4"
    if code_int == 42057: return "HMI_CFG_ScalaturaPortClampOn5"
    
    if code_int == 42059: return "HMI_CFG_ScalaturaTotClampOn1"
    if code_int == 42061: return "HMI_CFG_ScalaturaTotClampOn2"
    if code_int == 42063: return "HMI_CFG_ScalaturaTotClampOn3"
    if code_int == 42065: return "HMI_CFG_ScalaturaTotClampOn4"
    if code_int == 42067: return "HMI_CFG_ScalaturaTotClampOn5"
    
    if code_int == 42069: return "HMI_CFG_ScalaturaPortEM1"
    if code_int == 42071: return "HMI_CFG_ScalaturaPortEM2"
    if code_int == 42073: return "HMI_CFG_ScalaturaPortEM3"
    if code_int == 42075: return "HMI_CFG_ScalaturaPortEM4"
    if code_int == 42077: return "HMI_CFG_ScalaturaPortEM5"
    
    if code_int == 42079: return "HMI_CFG_ScalaturaTotEM1"
    if code_int == 42081: return "HMI_CFG_ScalaturaTotEM2"
    if code_int == 42083: return "HMI_CFG_ScalaturaTotEM3"
    if code_int == 42085: return "HMI_CFG_ScalaturaTotEM4"
    if code_int == 42087: return "HMI_CFG_ScalaturaTotEM5"
    
    if code_int == 42089: return "HMI_CFG_ScalaturaLivRadar"
    if code_int == 42091: return "HMI_CFG_SogliaAltaLvlRadar"
    if code_int == 42093: return "HMI_CFG_SogliaBassaLvlRadar"
    
    # DINT (42095-42110)
    if code_int == 42095: return "HMI_CFG_HBeg_1"
    if code_int == 42097: return "HMI_CFG_HEnd_1"
    if code_int == 42099: return "HMI_CFG_HBeg_2"
    if code_int == 42101: return "HMI_CFG_HEnd_2"
    if code_int == 42103: return "HMI_CFG_HBeg_3"
    if code_int == 42105: return "HMI_CFG_HEnd_3"
    if code_int == 42107: return "HMI_CFG_HBeg_4"
    if code_int == 42109: return "HMI_CFG_HEnd_4"
    
    # REAL (42111-42152)
    # PortataTotClampOn1-5 (42111-42119)
    if code_int == 42111: return "HMI_PortataTotClampOn1"
    if code_int == 42113: return "HMI_PortataTotClampOn2"
    if code_int == 42115: return "HMI_PortataTotClampOn3"
    if code_int == 42117: return "HMI_PortataTotClampOn4"
    if code_int == 42119: return "HMI_PortataTotClampOn5"

    # PortInstClampOn1-5 (42121-42129)
    if code_int == 42121: return "HMI_PortInstClampOn1"
    if code_int == 42123: return "HMI_PortInstClampOn2"
    if code_int == 42125: return "HMI_PortInstClampOn3"
    if code_int == 42127: return "HMI_PortInstClampOn4"
    if code_int == 42129: return "HMI_PortInstClampOn5"

    # PortataTotEM1-5 (42131-42139)
    if code_int == 42131: return "HMI_PortataTotEM1"
    if code_int == 42133: return "HMI_PortataTotEM2"
    if code_int == 42135: return "HMI_PortataTotEM3"
    if code_int == 42137: return "HMI_PortataTotEM4"
    if code_int == 42139: return "HMI_PortataTotEM5"

    # PortInstEM1-5 (42141-42149)
    if code_int == 42141: return "HMI_PortInstEM1"
    if code_int == 42143: return "HMI_PortInstEM2"
    if code_int == 42145: return "HMI_PortInstEM3"
    if code_int == 42147: return "HMI_PortInstEM4"
    if code_int == 42149: return "HMI_PortInstEM5"

    if code_int == 42151: return "HMI_LivelloRadarScalato"
    
    return f"TAG_{code_int}"

def get_alias_coil(offset):
    # 1-based offset given (e.g. 1 for 00001)
    if 1 <= offset <= 4: return f"DO_Valvola_{offset}"
    if 5 <= offset <= 8: return f"HMI_CMD_Valvola_{offset-4}"
    if 9 <= offset <= 12: return f"HMI_CFG_EnableValvola_{offset-8}"
    if 13 <= offset <= 17: return f"HMI_ResetTotClampOn{offset-12}"
    if 18 <= offset <= 22: return f"HMI_ResetTotEM{offset-17}"
    if offset == 23: return "HMI_Set_DateAndTime"
    return f"COIL_{offset}"

def get_alias_discrete(offset):
    # 1-based offset (1 for 10001)
    if 1 <= offset <= 5: return f"Totalizzatore_CLAMPON_{offset}"
    
    # 6=Aperta1, 7=Chiusa1, 8=Aperta2, 9=Chiusa2...
    if offset == 6: return "HMI_FC_Aperta_Valvola_1"
    if offset == 7: return "HMI_FC_Chiusa_Valvola_1"
    if offset == 8: return "HMI_FC_Aperta_Valvola_2"
    if offset == 9: return "HMI_FC_Chiusa_Valvola_2"
    if offset == 10: return "HMI_FC_Aperta_Valvola_3"
    if offset == 11: return "HMI_FC_Chiusa_Valvola_3"
    if offset == 12: return "HMI_FC_Aperta_Valvola_4"
    if offset == 13: return "HMI_FC_Chiusa_Valvola_4"
    
    if 14 <= offset <= 18: return f"Totalizzatore_EM_{offset-13}"
    return f"DISCRETE_{offset}"

def get_alias_input(offset):
    # 1-based offset (1 for 30001)
    if 1 <= offset <= 5: return f"Portata_Misuratore_CLAMP_ON_{offset}"
    if 6 <= offset <= 10: return f"Portata_Misuratore_EM_{offset-5}"
    if offset == 11: return "LivelloRadar"
    return f"INPUT_{offset}"

with open(filename, "w") as f:
    f.write("BEGIN;\n")
    # Clean up ALL ranges
    f.write("DELETE FROM tags WHERE code LIKE '0%';\n")
    f.write("DELETE FROM tags WHERE code LIKE '1%';\n")
    f.write("DELETE FROM tags WHERE code LIKE '3%';\n")
    f.write("DELETE FROM tags WHERE code LIKE '4%';\n") 
    f.write("\n")

    gateway_id = 1
    
    # 1. COILS (00001-00023) - BOOL
    for i in range(1, 24):
        code = f"{i:05d}" # 00001...
        alias = get_alias_coil(i)
        f.write(f"INSERT INTO tags (gateway_id, code, alias, data_type, historize, historize_deadband) VALUES ({gateway_id}, '{code}', '{alias}', 'BOOL', true, 0);\n")

    # 2. DISCRETE INPUTS (10001-10018) - BOOL
    for i in range(1, 19):
        code = f"1{i:04d}" # 10001...
        alias = get_alias_discrete(i)
        f.write(f"INSERT INTO tags (gateway_id, code, alias, data_type, historize, historize_deadband) VALUES ({gateway_id}, '{code}', '{alias}', 'BOOL', true, 0);\n")

    # 3. INPUT REGISTERS (30001-30011) - INT
    for i in range(1, 12):
        code = f"3{i:04d}" # 30001...
        alias = get_alias_input(i)
        f.write(f"INSERT INTO tags (gateway_id, code, alias, data_type, historize, historize_deadband) VALUES ({gateway_id}, '{code}', '{alias}', 'INT', true, 0);\n")

    # 4.1 BLOCCO QW (40001-40027) - INT
    for i in range(40001, 40028):
        offset = i - 40001
        alias = get_alias_qw(offset)
        f.write(f"INSERT INTO tags (gateway_id, code, alias, data_type, historize, historize_deadband) VALUES ({gateway_id}, '{i}', '{alias}', 'INT', true, 0);\n")

    # 4.2 BLOCCO MW (41025-41028) - INT
    mw_map = {41025: 'HMI_Read_Seconds', 41026: 'HMI_Read_Hour_Minute', 41027: 'HMI_Read_Month_Day', 41028: 'HMI_Read_Year'}
    for code, alias in mw_map.items():
        f.write(f"INSERT INTO tags (gateway_id, code, alias, data_type, historize, historize_deadband) VALUES ({gateway_id}, '{code}', '{alias}', 'INT', true, 0);\n")

    # 4.3 BLOCCO MD (42049-42152)
    # REAL: 42049-42094
    for i in range(42049, 42095, 2):
        alias = get_alias_md(i)
        f.write(f"INSERT INTO tags (gateway_id, code, alias, data_type, historize, historize_deadband) VALUES ({gateway_id}, '{i}', '{alias}', 'REAL', true, 0);\n")

    # DINT: 42095-42110
    for i in range(42095, 42111, 2):
        alias = get_alias_md(i)
        f.write(f"INSERT INTO tags (gateway_id, code, alias, data_type, historize, historize_deadband) VALUES ({gateway_id}, '{i}', '{alias}', 'DINT', true, 0);\n")

    # REAL: 42111-42152
    for i in range(42111, 42153, 2):
        alias = get_alias_md(i)
        f.write(f"INSERT INTO tags (gateway_id, code, alias, data_type, historize, historize_deadband) VALUES ({gateway_id}, '{i}', '{alias}', 'REAL', true, 0);\n")

    f.write("COMMIT;\n")
