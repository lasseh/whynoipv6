ALTER TABLE domain_resource
  DROP CONSTRAINT domain_resource_domain_id_fkey,
  ADD CONSTRAINT domain_resource_domain_id_fkey
    FOREIGN KEY (domain_id) REFERENCES domain(id) ON DELETE CASCADE;
DROP INDEX idx_domain_dependents_order;
DROP INDEX idx_asn_count_total;
DROP INDEX idx_asn_count_v6;
DROP INDEX idx_check_job_created;
