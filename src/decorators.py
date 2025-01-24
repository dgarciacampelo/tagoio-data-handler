from time import time
from loguru import logger


def benchmark(func):
    "A decorator to measure the execution time of a function, in seconds"

    def wrapper(*args, **kwargs):
        start_time = time()
        result = func(*args, **kwargs)
        end_time = time()
        elapsed_time = end_time - start_time

        logger.debug(f"Execution time for {func.__name__}: {elapsed_time:.3f} seconds")
        return result

    return wrapper
