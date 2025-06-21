import ctypes
import atexit
import sys
import os

def register_plugin(app_context):
    dll_path = os.path.abspath("shredder.dll")
    lib = ctypes.WinDLL(dll_path)
    lib.init_logger.restype = None
    lib.init_logger.argtypes = []
    lib.init_logger()
    lib.close_logger.restype = None
    lib.close_logger.argtypes = []
    atexit.register(lib.close_logger)
    lib.shred.restype = None
    lib.shred.argtypes = [ctypes.c_char_p, ctypes.c_int]
    def shred_file(path, passes, progress_callback=None):
        lib.shred(path.encode('utf-8'), passes)
        progress_callback(100)
        return True, "file has been cleaned up"
    main = sys.modules.get('__main__')
    if main:
        main.secure_shred_file = shred_file
    else:
        raise RuntimeError("Could not find __main__ module")
