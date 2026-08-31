# common folder

This folder contains common utility and code that shouldn't be part of the main application.    
It's intended to be an easy-access place for quick-and-clean implementations without the hassle of deploying new services.

These are not complete substitutes for dedicated solutions like RabbitMQ or S3; even though it should be perfectly suited for production environments.   

Use in production, just double-check if it covers all your use cases.


## But files in the database?

Yes, SQLite is embedded and the roundtrips are orders of magnitude cheaper then traditional server-based databases. `mmap` and `syncronous NORMAL` also do very good jobs at reducing latency, memory consumption and CPU usage, allowing for these implementations.
