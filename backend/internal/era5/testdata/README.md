# ERA5-Land test data

`era5-land-lisbon-t2m.csv` and `era5-land-lisbon-tp.csv` hold the header and
48 rows, 1991-01-06 00:00 to 1991-01-07 23:00 UTC, of the two CSV files
inside a Climate Data Store download of [ERA5 Land hourly time-series data
from 1950 to present](https://doi.org/10.24381/ee82e357). The download was
made on 2026-10-11 by `scripts/climate/download_era5.py` for Lisbon's own
coordinates (latitude 38.7247, longitude -9.1468), whose nearest grid point
is 38.7, -9.1, with the variables `2m_temperature` and
`total_precipitation`. The rows are copied unchanged. They include a small
negative precipitation value, values written with exponents, and an
afternoon of rain.

The results contain modified Copernicus Climate Change Service information
2026. Neither the European Commission nor ECMWF is responsible for any use
that may be made of the Copernicus information or data it contains. The
dataset is licensed under [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/).
